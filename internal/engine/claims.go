package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/policy"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

// Clock also controls maintenance scheduling; tests can advance it without sleeps.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type sessionState struct {
	Actor       api.Actor
	Connections int
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("OS random source: " + err.Error())
	}
	return prefix + hex.EncodeToString(b[:])
}

func (s *Engine) runtimeSnapshot() corestate.Runtime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtime.Clone()
}

// configMu serializes runtime transactions and saved configuration application.
// Never publish an ownership change until its atomic write has succeeded.
func (s *Engine) commitRuntime(st corestate.Runtime) error {
	if err := s.lifetimeError(); err != nil {
		return err
	}
	if err := s.runtimeStore.Save(st); err != nil {
		return fmt.Errorf("runtime.json: %w", err)
	}
	return s.applyWhileAlive(func() {
		s.runtimeMu.Lock()
		s.runtime = st
		s.runtimeMu.Unlock()
		s.notify()
	})
}

func (s *Engine) merged(cfg model.Config) model.Config {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	cfg.Tunnels = append(append([]model.Tunnel(nil), cfg.Tunnels...), s.runtime.Ephemeral...)
	return cfg
}

func (s *Engine) savedConfig() model.Config {
	cfg := s.manager.Config()
	out := cfg.Tunnels[:0:0]
	for _, t := range cfg.Tunnels {
		if !strings.HasPrefix(t.ID, "tmp-") {
			out = append(out, t)
		}
	}
	cfg.Tunnels = out
	return cfg
}

func activeState(st supervisor.State) bool {
	return st == supervisor.StateStarting || st == supervisor.StateRunning || st == supervisor.StateReconnecting
}

func (s *Engine) known(id string) bool {
	for _, t := range s.manager.Config().Tunnels {
		if t.ID == id {
			return true
		}
	}
	return false
}
func (s *Engine) running(id string) bool {
	for _, st := range s.manager.RuntimeStatuses() {
		if st.ID == id && activeState(st.State) {
			return true
		}
	}
	return s.hasOrphan(id)
}
func (s *Engine) hasOrphan(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.orphans {
		if r.TunnelID == id {
			return true
		}
	}
	return false
}

func (s *Engine) owner(id string) api.TunnelOwner {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	owner := s.runtime.Owners[id]
	o := api.TunnelOwner{Pinned: owner.Pinned, Ephemeral: strings.HasPrefix(id, "tmp-")}
	if owner.Holder != nil {
		holder := *owner.Holder
		o.Holder = &holder
	}
	return o
}

func (s *Engine) personStart(id string, restart bool) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if !s.known(id) {
		return fmt.Errorf("unknown tunnel %q", id)
	}
	if err := s.startPreflight(id, !s.running(id)); err != nil {
		return err
	}
	st := s.runtimeSnapshot()
	owner := st.Owners[id]
	owner.Pinned = true
	st.Owners[id] = owner
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	if restart {
		if err := s.stopProcesses(id); err != nil {
			return err
		}
		if err := s.startPreflight(id, true); err != nil {
			return err
		}
	} else if s.hasOrphan(id) {
		return nil
	}
	return s.manager.Start(id)
}

// StopTunnel is the person-only stop: persist revocation before stopping children.
func (s *Engine) StopTunnel(id string) error {
	return s.StopTunnelFor(api.Actor{Kind: "gui", Name: "person"}, id)
}
func (s *Engine) StopTunnelFor(actor api.Actor, id string) error {
	if actor.Kind == "mcp" {
		return s.policyDenied(actor, id)
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	if !s.known(id) {
		return fmt.Errorf("unknown tunnel %q", id)
	}
	st := s.runtimeSnapshot()
	// Keep a durable cleanup intent until the process has actually stopped.
	st.Owners[id] = corestate.RuntimeOwner{Started: true}
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	if err := s.stopProcesses(id); err != nil {
		return err
	}
	delete(st.Owners, id)
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	return errors.Join(s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelStop, TunnelID: id, Result: audit.ResultOK}), s.removeEphemeralLocked(id))
}

func (s *Engine) stopProcesses(id string) error {
	s.manager.Stop(id)
	s.mu.Lock()
	recs := append([]supervisor.Record(nil), s.orphans...)
	s.mu.Unlock()
	for _, r := range recs {
		if r.TunnelID == id {
			if err := s.StopOrphan(r.PID); err != nil {
				return err
			}
		}
	}
	return nil
}

// OpenSaved starts a saved tunnel for a client, or reuses one that is already
// running. The client holds the tunnel only when it started it.
func (s *Engine) OpenSaved(actor api.Actor, session string, p api.OpenSavedParams) (string, error) {
	return s.OpenSavedContext(context.Background(), actor, session, p)
}
func (s *Engine) OpenSavedContext(ctx context.Context, actor api.Actor, session string, p api.OpenSavedParams) (string, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.openSavedLocked(ctx, actor, session, p)
}
func (s *Engine) openSavedLocked(ctx context.Context, actor api.Actor, session string, p api.OpenSavedParams) (string, error) {
	if err := s.mutationEnabled(actor, session); err != nil {
		return "", err
	}
	if err := s.validSession(actor, session); err != nil {
		return "", err
	}
	if !s.known(p.ID) {
		return "", fmt.Errorf("unknown tunnel %q", p.ID)
	}
	if err := s.agentTunnelCapacity(actor, session); err != nil {
		return "", err
	}
	var fingerprint string
	if actor.Kind == "mcp" {
		var t model.Tunnel
		// Opening an ephemeral ID is not an OpenSaved allow-list grant. Agents
		// acquire it only when it belongs to this session.
		for _, candidate := range s.savedConfig().Tunnels {
			if candidate.ID == p.ID {
				t = candidate
				break
			}
		}
		if t.ID == "" {
			return "", s.denySession(actor, session, p.ID, "")
		}
		var err error
		fingerprint, err = s.authorize(actor, session, policy.ActionOpenSaved, t, p.Purpose, approvalOperationKey(ctx, requestKey(struct{ Method, ID string }{"tunnel.open", p.ID})))
		if err != nil {
			return "", err
		}
	}
	started := !s.running(p.ID)
	if started {
		st := s.runtimeSnapshot()
		holder := actor
		st.Owners[p.ID] = corestate.RuntimeOwner{Pinned: st.Owners[p.ID].Pinned, Started: true, Holder: &holder, Session: session, Detached: actor.Kind == "mcp"}
		if err := s.commitRuntime(st); err != nil {
			return "", err
		}
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelOpen, TunnelID: p.ID, Fingerprint: fingerprint, Decision: audit.Allow, Result: audit.ResultOK}); err != nil {
		if started {
			_ = s.releaseLocked(actor, session, p.ID, true)
		}
		return "", err
	}
	if started {
		if err := s.startAuthorized(p.ID, fingerprint); err != nil {
			_ = s.releaseLocked(actor, session, p.ID, true)
			return "", err
		}
	}
	return p.ID, nil
}

func (s *Engine) validSession(actor api.Actor, session string) error {
	if session == "" || actor.Kind == "" || actor.Name == "" {
		return errors.New("a connected client session is required")
	}
	active, ok := s.sessions[session]
	if !ok || active.Connections == 0 || active.Actor != actor {
		return errors.New("client session is not connected")
	}
	return s.lifetimeError()
}

func holds(o corestate.RuntimeOwner, actor api.Actor, session string) bool {
	return o.Holder != nil && *o.Holder == actor && o.Session == session
}

// holdsTunnel reports whether the session still holds the tunnel it opened.
func (s *Engine) holdsTunnel(actor api.Actor, session, id string) bool {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return holds(s.runtime.Owners[id], actor, session)
}

// ReleaseTunnel gives up a tunnel the caller opened. The tunnel stops unless a
// person pinned it. An MCP client may also release a tunnel another MCP client
// opened, so a later session can clean up after an earlier one.
func (s *Engine) ReleaseTunnel(actor api.Actor, session, id string) error {
	if err := s.mutationEnabled(actor, session); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.validSession(actor, session); err != nil {
		return err
	}
	return s.releaseLocked(actor, session, id, false)
}

// releaseLocked drops the holder of id. With force it does not check that the
// caller is the holder, for cleanup after a failed start or a disconnect.
func (s *Engine) releaseLocked(actor api.Actor, session, id string, force bool) error {
	st := s.runtimeSnapshot()
	owner := st.Owners[id]
	if owner.Holder == nil {
		return api.NewError(api.Unknown, fmt.Sprintf("tunnel %q is not held by a client", id))
	}
	if !force && !holds(owner, actor, session) && !(actor.Kind == "mcp" && owner.Holder.Kind == "mcp") {
		return errors.New("tunnel belongs to another client")
	}
	owner.Holder, owner.Session, owner.Detached = nil, "", false
	st.Owners[id] = owner
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	auditErr := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelRelease, TunnelID: id, Result: audit.ResultOK})
	// An audit failure must not leave client-owned exposure running after release.
	return errors.Join(auditErr, s.stopUnownedLocked(id))
}

func (s *Engine) stopUnownedLocked(id string) error {
	st := s.runtimeSnapshot()
	owner := st.Owners[id]
	if owner.Pinned || !owner.Started || owner.Holder != nil {
		return nil
	}
	if err := s.stopProcesses(id); err != nil {
		return err
	}
	delete(st.Owners, id)
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	return s.removeEphemeralLocked(id)
}

// ConnectSession registers a real connection. Tokens must be retained by
// reconnecting adapters; equal names without the token are a new session.
func (s *Engine) ConnectSession(actor api.Actor, session string) (string, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if actor.Kind != "cli" && actor.Kind != "gui" && actor.Kind != "mcp" {
		return "", errors.New("client kind must be cli, gui or mcp")
	}
	if !audit.ValidActorName(actor.Name) {
		return "", errors.New("client name must be nonempty printable UTF-8 and at most 256 bytes")
	}
	if session == "" {
		session = randomID("ss_")
	}
	if len(session) > 256 {
		return "", errors.New("session token is too long")
	}
	current := s.sessions[session]
	if current.Connections > 0 && current.Actor != actor {
		return "", errors.New("session belongs to another client")
	}
	for _, owner := range s.runtimeSnapshot().Owners {
		if owner.Session == session && owner.Holder != nil && *owner.Holder != actor {
			return "", errors.New("session belongs to another client")
		}
	}
	if err := s.lifetimeError(); err != nil {
		return "", err
	}
	current.Actor = actor
	current.Connections++
	s.sessions[session] = current
	return session, nil
}

// DisconnectSession releases the tunnels the session opened, except detached
// ones, which keep running until a person or the client stops them.
func (s *Engine) DisconnectSession(actor api.Actor, session string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current := s.sessions[session]
	if current.Connections == 0 || current.Actor != actor {
		return nil
	}
	current.Connections--
	if current.Connections > 0 {
		s.sessions[session] = current
		return nil
	}
	delete(s.sessions, session)
	auditErr := s.flushDenials(session, true)
	var ids []string
	for id, owner := range s.runtimeSnapshot().Owners {
		if holds(owner, actor, session) && !owner.Detached {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		auditErr = errors.Join(auditErr, s.releaseLocked(actor, session, id, true))
	}
	return auditErr
}

func (s *Engine) managerChanged() {
	s.notify()
	select {
	case s.runtimeChanged <- struct{}{}:
	default:
	}
}
func (s *Engine) monitorRuntime() {
	for {
		select {
		case <-s.done:
			return
		case <-s.runtimeChanged:
		case <-s.clock.After(time.Second):
		}
		if err := s.Maintain(); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("runtime maintenance: %v", err)
		}
	}
}

// Maintain stops tunnels nobody holds any more and removes terminal ephemeral
// definitions. It is public so a scheduler or a deterministic test can
// synchronously apply an advanced clock.
func (s *Engine) Maintain() error {
	s.pruneOpenRequests()
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	auditErr := errors.Join(s.expireApprovalsLocked(), s.flushDenials("", false))
	s.pruneApprovalsLocked()
	if s.orphanCount.Load() > 0 {
		s.refreshOrphans()
	}
	// A crash can occur after the last release was persisted but before Stop.
	// Reconcile every client-started owner.
	for id, owner := range s.runtimeSnapshot().Owners {
		if owner.Started && owner.Holder == nil {
			auditErr = errors.Join(auditErr, s.stopUnownedLocked(id))
		}
	}
	for _, t := range s.runtimeSnapshot().Ephemeral {
		if !s.running(t.ID) && !s.manager.WaitingForSecret(t.ID) {
			auditErr = errors.Join(auditErr, s.removeEphemeralLocked(t.ID))
		}
	}
	return auditErr
}

func (s *Engine) clearRuntimeOnShutdown() {
	// Shutdown must not wait for blocked configuration/keychain I/O.
	if !s.configMu.TryLock() {
		return
	}
	defer s.configMu.Unlock()
	st := s.runtimeSnapshot()
	kept := []model.Tunnel{}
	for _, tunnel := range st.Ephemeral {
		if s.hasOrphan(tunnel.ID) {
			kept = append(kept, tunnel)
		}
	}
	st.Ephemeral = kept
	st.Owners = map[string]corestate.RuntimeOwner{}
	s.mu.Lock()
	for _, orphan := range s.orphans {
		// A failed OS stop remains a cleanup intent for the next daemon.
		st.Owners[orphan.TunnelID] = corestate.RuntimeOwner{Started: true}
	}
	s.mu.Unlock()
	st.AutoPorts = map[string][]int{}
	if err := s.runtimeStore.Save(st); err != nil {
		log.Printf("runtime shutdown: %v", err)
		return
	}
	s.runtimeMu.Lock()
	s.runtime = st
	s.runtimeMu.Unlock()
}

func (s *Engine) startAuthorized(id, fingerprint string) error {
	if err := s.startPreflight(id, !s.running(id)); err != nil {
		return err
	}
	if fingerprint != "" {
		return s.manager.StartFingerprint(id, fingerprint)
	}
	return s.manager.Start(id)
}
