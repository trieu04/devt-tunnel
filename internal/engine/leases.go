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

const DefaultLeaseTTL = 30 * time.Minute
const MaxLeaseTTL = 8 * time.Hour
const DisconnectGrace = 2 * time.Minute

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

func leaseTTL(value string) (time.Duration, error) {
	if value == "" {
		return DefaultLeaseTTL, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 || d > MaxLeaseTTL {
		return 0, fmt.Errorf("TTL must be positive and at most %s", MaxLeaseTTL)
	}
	return d, nil
}

func (s *Engine) leaseTTL(actor api.Actor, value string) (time.Duration, error) {
	if actor.Kind != "mcp" {
		return leaseTTL(value)
	}
	a := s.agentSettings().AgentPolicy()
	if err := a.Validate(); err != nil {
		return 0, api.NewError(api.PolicyDenied, "invalid agent policy")
	}
	if value == "" {
		value = a.DefaultTTL
	}
	ttl, err := time.ParseDuration(value)
	max, _ := time.ParseDuration(a.MaxTTL)
	if err != nil || ttl <= 0 || ttl > max {
		return 0, fmt.Errorf("TTL must be positive and at most %s", max)
	}
	return ttl, nil
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
	o := api.TunnelOwner{Pinned: s.runtime.Owners[id].Pinned, Ephemeral: strings.HasPrefix(id, "tmp-")}
	for _, l := range s.runtime.Leases {
		if l.TunnelID != id || l.Revoked {
			continue
		}
		o.Leases++
		expiry := l.ExpiresAt
		if l.GraceUntil != nil && l.GraceUntil.Before(expiry) {
			expiry = *l.GraceUntil
		}
		if o.Holder == nil {
			holder := l.Holder
			o.Holder = &holder
		}
		if !expiry.IsZero() && (o.Expiry == nil || expiry.Before(*o.Expiry)) {
			holder := l.Holder
			o.Holder = &holder
			o.Expiry = &expiry
		}
	}
	return o
}

func (s *Engine) personStart(id string, restart bool) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if !s.known(id) {
		return fmt.Errorf("unknown tunnel %q", id)
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
	var auditErr error
	st := s.runtimeSnapshot()
	for i := range st.Leases {
		if st.Leases[i].TunnelID == id {
			if !st.Leases[i].Revoked {
				auditErr = errors.Join(auditErr, s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.LeaseRevoke, TunnelID: id, LeaseID: st.Leases[i].ID, Result: audit.ResultRevoked}))
			}
			st.Leases[i].Revoked = true
		}
	}
	// Keep a durable cleanup intent until the process has actually stopped.
	st.Owners[id] = corestate.RuntimeOwner{LeaseStarted: true}
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
	return errors.Join(auditErr, s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelStop, TunnelID: id, Result: audit.ResultOK}), s.removeEphemeralLocked(id))
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

func (s *Engine) OpenLease(actor api.Actor, session string, p api.OpenLeaseParams) (api.Lease, error) {
	return s.OpenLeaseContext(context.Background(), actor, session, p)
}
func (s *Engine) OpenLeaseContext(ctx context.Context, actor api.Actor, session string, p api.OpenLeaseParams) (api.Lease, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return api.Lease{}, err
	}
	return s.openLeaseLocked(actor, session, p)
}
func (s *Engine) openLeaseLocked(actor api.Actor, session string, p api.OpenLeaseParams) (api.Lease, error) {
	if err := s.mutationEnabled(actor, session); err != nil {
		return api.Lease{}, err
	}
	ttl, err := s.leaseTTL(actor, p.TTL)
	if err != nil {
		return api.Lease{}, err
	}
	if err := s.validSession(actor, session); err != nil {
		return api.Lease{}, err
	}
	if !s.known(p.ID) {
		return api.Lease{}, fmt.Errorf("unknown tunnel %q", p.ID)
	}
	if err := s.agentLeaseCapacity(actor, session); err != nil {
		return api.Lease{}, err
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
			return api.Lease{}, s.denySession(actor, session, p.ID, "")
		}
		fingerprint, err = s.authorize(actor, session, policy.ActionOpenSaved, t, p.Purpose, requestKey(struct{ Method, ID string }{"lease.open", p.ID}))
		if err != nil {
			return api.Lease{}, err
		}
	}
	st := s.runtimeSnapshot()
	started := !s.running(p.ID)
	l := api.Lease{ID: randomID("ls_"), TunnelID: p.ID, Holder: actor, Session: session, CreatedAt: s.clock.Now(), ExpiresAt: s.clock.Now().Add(ttl), StartedIt: started}
	owner := st.Owners[p.ID]
	owner.LeaseStarted = owner.LeaseStarted || started
	st.Owners[p.ID] = owner
	st.Leases = append(st.Leases, l)
	if err := s.commitRuntime(st); err != nil {
		return api.Lease{}, err
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.LeaseOpen, TunnelID: p.ID, LeaseID: l.ID, Fingerprint: fingerprint, Decision: audit.Allow, Result: audit.ResultOK}); err != nil {
		_ = s.closeLeaseLocked(actor, session, l.ID, false)
		return api.Lease{}, err
	}
	if started {
		if err := s.startAuthorized(p.ID, fingerprint); err != nil {
			_ = s.closeLeaseLocked(actor, session, l.ID, false)
			return api.Lease{}, err
		}
	}
	return l, nil
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

func ownsLease(l api.Lease, actor api.Actor, session string) bool {
	return l.Holder == actor && l.Session == session
}

func (s *Engine) RenewLease(actor api.Actor, session string, p api.RenewLeaseParams) (api.Lease, error) {
	if err := s.mutationEnabled(actor, session); err != nil {
		return api.Lease{}, err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.validSession(actor, session); err != nil {
		return api.Lease{}, err
	}
	ttl, err := s.leaseTTL(actor, p.TTL)
	if err != nil {
		return api.Lease{}, err
	}
	st := s.runtimeSnapshot()
	for i := range st.Leases {
		l := &st.Leases[i]
		if l.ID != p.ID {
			continue
		}
		if !ownsLease(*l, actor, session) {
			return api.Lease{}, errors.New("lease belongs to another client")
		}
		if l.Revoked {
			return api.Lease{}, api.NewError(api.LeaseRevoked, "lease was revoked by a person")
		}
		if !leaseAlive(*l, s.clock.Now()) {
			return api.Lease{}, api.NewError(api.Stopped, "lease has expired")
		}
		l.ExpiresAt = s.clock.Now().Add(ttl)
		out := *l
		if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.LeaseRenew, TunnelID: l.TunnelID, LeaseID: l.ID, Result: audit.ResultOK}); err != nil {
			return api.Lease{}, err
		}
		if err := s.commitRuntime(st); err != nil {
			return api.Lease{}, err
		}
		return out, nil
	}
	return api.Lease{}, fmt.Errorf("unknown lease %q", p.ID)
}

func (s *Engine) CloseLease(actor api.Actor, session, id string) error {
	if err := s.mutationEnabled(actor, session); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.validSession(actor, session); err != nil {
		return err
	}
	return s.closeLeaseLocked(actor, session, id, false)
}
func (s *Engine) RevokeLease(id string) error {
	return s.RevokeLeaseFor(api.Actor{Kind: "gui", Name: "person"}, id)
}
func (s *Engine) RevokeLeaseFor(actor api.Actor, id string) error {
	if actor.Kind == "mcp" {
		return s.policyDenied(actor, "")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.closeLeaseLocked(actor, "", id, true)
}
func (s *Engine) closeLeaseLocked(actor api.Actor, session, id string, revoke bool) error {
	st := s.runtimeSnapshot()
	var tunnelID string
	for i, l := range st.Leases {
		if l.ID != id {
			continue
		}
		if !revoke && !ownsLease(l, actor, session) {
			return errors.New("lease belongs to another client")
		}
		if l.Revoked {
			return api.NewError(api.LeaseRevoked, "lease was revoked by a person")
		}
		tunnelID = l.TunnelID
		if revoke {
			st.Leases[i].Revoked = true
		} else {
			st.Leases = append(st.Leases[:i], st.Leases[i+1:]...)
		}
		break
	}
	if tunnelID == "" {
		return fmt.Errorf("unknown lease %q", id)
	}
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	auditAction, result := audit.LeaseClose, audit.ResultOK
	if revoke {
		auditAction, result = audit.LeaseRevoke, audit.ResultRevoked
	}
	auditErr := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: auditAction, TunnelID: tunnelID, LeaseID: id, Result: result})
	// An audit failure must not leave lease-owned exposure running after release.
	return errors.Join(auditErr, s.stopUnownedLocked(tunnelID))
}

func (s *Engine) ListLeases(actor api.Actor, session string, ownOnly bool) []api.Lease {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	out := []api.Lease{}
	for _, l := range s.runtime.Leases {
		if !ownOnly || ownsLease(l, actor, session) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func leaseAlive(l api.Lease, now time.Time) bool {
	return !l.Revoked && ((l.Detached && l.ExpiresAt.IsZero()) || now.Before(l.ExpiresAt)) && (l.GraceUntil == nil || now.Before(*l.GraceUntil))
}

func (s *Engine) stopUnownedLocked(id string) error {
	st := s.runtimeSnapshot()
	owner := st.Owners[id]
	if owner.Pinned || !owner.LeaseStarted {
		return nil
	}
	for _, l := range st.Leases {
		if l.TunnelID == id && !l.Revoked {
			return nil
		}
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

// ConnectSession binds all claims to a real connection. Tokens must be retained
// by reconnecting adapters; equal names without the token do not resume leases.
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
	st := s.runtimeSnapshot()
	changed := false
	now := s.clock.Now()
	for i := range st.Leases {
		l := &st.Leases[i]
		if l.Session != session {
			continue
		}
		if l.Holder != actor {
			return "", errors.New("session belongs to another client")
		}
		if !l.Revoked && !leaseAlive(*l, now) {
			continue
		}
		if l.GraceUntil != nil {
			l.GraceUntil = nil
			changed = true
		}
	}
	if changed {
		if err := s.commitRuntime(st); err != nil {
			return "", err
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
	st := s.runtimeSnapshot()
	changed := false
	until := s.clock.Now().Add(DisconnectGrace)
	for i := range st.Leases {
		l := &st.Leases[i]
		if ownsLease(*l, actor, session) && !l.Revoked && !l.Detached {
			l.GraceUntil = &until
			changed = true
		}
	}
	if changed {
		return errors.Join(auditErr, s.commitRuntime(st))
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
		if err := s.ExpireLeases(); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("runtime maintenance: %v", err)
		}
	}
}

// ExpireLeases also removes terminal ephemeral definitions. It is public so a
// scheduler or a deterministic test can synchronously apply an advanced clock.
func (s *Engine) ExpireLeases() error {
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
	st := s.runtimeSnapshot()
	changed := false
	kept := st.Leases[:0]
	affected := map[string]bool{}
	for _, l := range st.Leases {
		// Retain revocation tombstones until their original TTL for the next call.
		if (!l.ExpiresAt.IsZero() && !s.clock.Now().Before(l.ExpiresAt)) || (l.Revoked && l.ExpiresAt.IsZero()) || (!l.Revoked && !leaseAlive(l, s.clock.Now())) {
			if !l.Revoked {
				auditErr = errors.Join(auditErr, s.recordAudit(audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.LeaseExpire, TunnelID: l.TunnelID, LeaseID: l.ID, Result: audit.ResultExpired}))
			}
			affected[l.TunnelID] = true
			changed = true
		} else {
			kept = append(kept, l)
		}
	}
	st.Leases = kept
	if changed {
		if err := s.commitRuntime(st); err != nil {
			return err
		}
	}
	// A crash can occur after the last close was persisted but before Stop.
	// Reconcile all lease-started owners, not only leases removed this tick.
	for id, owner := range s.runtimeSnapshot().Owners {
		if owner.LeaseStarted {
			affected[id] = true
		}
	}
	for id := range affected {
		auditErr = errors.Join(auditErr, s.stopUnownedLocked(id))
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
		st.Owners[orphan.TunnelID] = corestate.RuntimeOwner{LeaseStarted: true}
	}
	s.mu.Unlock()
	st.AutoPorts = map[string][]int{}
	for i := range st.Leases {
		st.Leases[i].Revoked = true
	}
	if err := s.runtimeStore.Save(st); err != nil {
		log.Printf("runtime shutdown: %v", err)
		return
	}
	s.runtimeMu.Lock()
	s.runtime = st
	s.runtimeMu.Unlock()
}

func (s *Engine) startAuthorized(id, fingerprint string) error {
	if fingerprint != "" {
		return s.manager.StartFingerprint(id, fingerprint)
	}
	return s.manager.Start(id)
}
