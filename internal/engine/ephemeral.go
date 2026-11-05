package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/policy"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
)

func (s *Engine) RunEphemeral(actor api.Actor, session string, p api.RunEphemeralParams) (api.RunEphemeralResult, error) {
	return s.RunEphemeralContext(context.Background(), actor, session, p)
}
func (s *Engine) RunEphemeralContext(ctx context.Context, actor api.Actor, session string, p api.RunEphemeralParams) (api.RunEphemeralResult, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.validSession(actor, session); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.mutationEnabled(actor, session); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.agentBuilt(actor, session, p.Tunnel); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.agentLeaseCapacity(actor, session); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if p.Detached && actor.Kind != "cli" && actor.Kind != "gui" {
		return api.RunEphemeralResult{}, s.denySession(actor, session, "", "")
	}
	ttl, err := s.leaseTTL(actor, p.TTL)
	if err != nil {
		return api.RunEphemeralResult{}, err
	}
	key := requestKey(struct {
		Method string
		Tunnel model.Tunnel
	}{"tunnel.runEphemeral", p.Tunnel})
	t := p.Tunnel
	prepared := false
	if actor.Kind == "mcp" {
		if previous, ok := s.preparedEphemeral(actor, session, key); ok {
			t = previous
			prepared = true
		}
	}
	// Copy the caller's mutable nested fields before port allocation.
	copy := corestate.Runtime{Ephemeral: []model.Tunnel{t}}.Clone()
	t = copy.Ephemeral[0]
	if t.ID == "" {
		t.ID = randomID("tmp-")
	}
	defer func() {
		if !s.known(t.ID) {
			delete(s.portHistory, t.ID)
		}
	}()
	if !strings.HasPrefix(t.ID, "tmp-") {
		return api.RunEphemeralResult{}, errors.New("ephemeral ids must start with tmp-")
	}
	if s.known(t.ID) {
		return api.RunEphemeralResult{}, fmt.Errorf("duplicate tunnel id %q", t.ID)
	}
	var auto []int
	if !prepared {
		auto, err = s.allocatePorts(&t, nil)
		if err != nil {
			return api.RunEphemeralResult{}, err
		}
	}
	cfg := s.manager.Config()
	cfg.Tunnels = append(append([]model.Tunnel(nil), cfg.Tunnels...), t)
	if err := cfg.Validate(); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if _, err := s.manager.PreviewDraft(t); err != nil {
		return api.RunEphemeralResult{}, err
	}
	action := policy.ActionCreate
	if policy.PublicExposure(t) {
		action = policy.ActionExpose
	}
	fingerprint, err := s.authorize(actor, session, action, t, p.Purpose, key)
	if err != nil {
		return api.RunEphemeralResult{}, err
	}
	st := s.runtimeSnapshot()
	l := api.Lease{ID: randomID("ls_"), TunnelID: t.ID, Holder: actor, Session: session, CreatedAt: s.clock.Now(), ExpiresAt: s.clock.Now().Add(ttl), StartedIt: true, Detached: p.Detached}
	if p.Detached && p.TTL == "" {
		l.ExpiresAt = time.Time{}
	}
	st.Ephemeral = append(st.Ephemeral, t)
	st.Leases = append(st.Leases, l)
	st.Owners[t.ID] = corestate.RuntimeOwner{LeaseStarted: true}
	if len(auto) > 0 {
		st.AutoPorts[t.ID] = auto
	}
	if err := s.commitRuntime(st); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.applyWhileAlive(func() { s.setManagerConfig(cfg) }); err != nil {
		return api.RunEphemeralResult{}, err
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelRunEphemeral, TunnelID: t.ID, LeaseID: l.ID, Fingerprint: fingerprint, Decision: audit.Allow, Result: audit.ResultOK}); err != nil {
		_ = s.removeEphemeralLocked(t.ID)
		return api.RunEphemeralResult{}, err
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.LeaseOpen, TunnelID: t.ID, LeaseID: l.ID, Fingerprint: fingerprint, Result: audit.ResultOK}); err != nil {
		_ = s.removeEphemeralLocked(t.ID)
		return api.RunEphemeralResult{}, err
	}
	if err := s.startAuthorized(t.ID, fingerprint); err != nil {
		_ = s.removeEphemeralLocked(t.ID)
		return api.RunEphemeralResult{}, err
	}
	return api.RunEphemeralResult{ID: t.ID, Lease: l}, nil
}

func (s *Engine) removeEphemeralLocked(id string) error {
	st := s.runtimeSnapshot()
	found := false
	kept := st.Ephemeral[:0]
	for _, t := range st.Ephemeral {
		if t.ID == id {
			found = true
		} else {
			kept = append(kept, t)
		}
	}
	if !found {
		return nil
	}
	st.Ephemeral = kept
	delete(s.portHistory, id)
	delete(st.AutoPorts, id)
	delete(st.Owners, id)
	var revocations []audit.Event
	for i := range st.Leases {
		if st.Leases[i].TunnelID == id {
			if !st.Leases[i].Revoked {
				revocations = append(revocations, audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.LeaseRevoke, TunnelID: id, LeaseID: st.Leases[i].ID, Result: audit.ResultRevoked})
			}
			st.Leases[i].Revoked = true
		}
	}
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	cfg := s.savedConfig()
	if err := s.applyWhileAlive(func() { s.setManagerConfig(s.merged(cfg)) }); err != nil {
		return err
	}
	var auditErr error
	for _, event := range revocations {
		auditErr = errors.Join(auditErr, s.recordAudit(event))
	}
	return errors.Join(auditErr, s.recordAudit(audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.TunnelStopEphemeral, TunnelID: id, Result: audit.ResultOK}))
}

// PromoteTunnel renames the supervisor entry in place, preserving process,
// fingerprint and leases. A write-ahead intent repairs crashes between files.
func (s *Engine) PromoteTunnel(p api.PromoteTunnelParams) (string, error) {
	return s.PromoteTunnelFor(api.Actor{Kind: "gui", Name: "person"}, "", p)
}
func (s *Engine) PromoteTunnelFor(actor api.Actor, session string, p api.PromoteTunnelParams) (string, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return "", err
	}
	st := s.runtimeSnapshot()
	var t model.Tunnel
	for _, candidate := range st.Ephemeral {
		if candidate.ID == p.ID {
			t = candidate
			break
		}
	}
	if t.ID == "" {
		return "", fmt.Errorf("unknown ephemeral tunnel %q", p.ID)
	}
	if p.Name != "" {
		t.Name = p.Name
	}
	taken := map[string]bool{}
	for _, candidate := range s.manager.Config().Tunnels {
		taken[candidate.ID] = true
	}
	t.ID = p.SavedID
	if t.ID == "" {
		t.ID = model.NewID(t.Name, taken)
		if strings.HasPrefix(t.ID, "tmp-") {
			t.ID = model.NewID("saved-"+t.Name, taken)
		}
	}
	if strings.HasPrefix(t.ID, "tmp-") {
		return "", errors.New("saved ids may not start with tmp-")
	}
	if taken[t.ID] {
		return "", fmt.Errorf("duplicate tunnel id %q", t.ID)
	}
	if actor.Kind == "mcp" {
		if err := s.validSession(actor, session); err != nil {
			return "", err
		}
		owns := false
		for _, l := range st.Leases {
			if l.TunnelID == p.ID && ownsLease(l, actor, session) && leaseAlive(l, s.clock.Now()) {
				owns = true
			}
		}
		if !owns {
			return "", s.denySession(actor, session, p.ID, "")
		}
	}
	if err := s.agentBuilt(actor, session, t); err != nil {
		return "", err
	}
	key := requestKey(struct{ Method, ID, SavedID, Name string }{"tunnel.promote", p.ID, t.ID, t.Name})
	fingerprint, err := s.authorize(actor, session, policy.ActionSave, t, p.Purpose, key)
	if err != nil {
		return "", err
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelPromote, TunnelID: t.ID, Fingerprint: fingerprint, Result: audit.ResultOK}); err != nil {
		return "", err
	}
	st.Promotions[p.ID] = t.ID
	if err := s.commitRuntime(st); err != nil {
		return "", err
	}
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		cfg.Tunnels = append(cfg.Tunnels, t)
		return s.merged(*cfg).Validate()
	})
	if err != nil {
		delete(st.Promotions, p.ID)
		_ = s.commitRuntime(st)
		return "", err
	}
	// Publish the rename even if final runtime cleanup fails: the intent makes it
	// recoverable, and the saved file is now committed. No process is restarted.
	if err := s.applyWhileAlive(func() { s.renameRedaction(p.ID, t.ID); s.manager.Rename(p.ID, t.ID) }); err != nil {
		return t.ID, err
	}
	// Recovery records must be renamed before clearing the write-ahead intent.
	delete(s.portHistory, p.ID)
	recordErr := s.renameRecords(p.ID, t.ID)
	remapRuntime(&st, p.ID, t.ID)
	if recordErr != nil {
		st.Promotions[p.ID] = t.ID
	}
	runtimeErr := s.commitRuntime(st)
	if runtimeErr != nil {
		// The saved definition and durable intent already committed. Reflect that
		// move in memory too; a later write/restart finishes runtime cleanup.
		s.runtimeMu.Lock()
		s.runtime = st
		s.runtimeMu.Unlock()
	}
	if err := s.applyWhileAlive(func() { s.setManagerConfig(s.merged(cfg)) }); err != nil {
		return t.ID, err
	}
	if recordErr != nil {
		return t.ID, recordErr
	}
	return t.ID, runtimeErr
}

func remapRuntime(st *corestate.Runtime, from, to string) {
	kept := st.Ephemeral[:0]
	for _, t := range st.Ephemeral {
		if t.ID != from {
			kept = append(kept, t)
		}
	}
	st.Ephemeral = kept
	if o, ok := st.Owners[from]; ok {
		st.Owners[to] = o
		delete(st.Owners, from)
	}
	for i := range st.Leases {
		if st.Leases[i].TunnelID == from {
			st.Leases[i].TunnelID = to
		}
	}
	delete(st.AutoPorts, from)
	delete(st.Promotions, from)
}

func (s *Engine) renameRecords(from, to string) error {
	if err := s.records.RenameTunnel(from, to); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.orphans {
		if s.orphans[i].TunnelID == from {
			s.orphans[i].TunnelID = to
		}
	}
	return nil
}

func (s *Engine) recoverPromotions() {
	st := s.runtimeSnapshot()
	if len(st.Promotions) == 0 {
		return
	}
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return
	}
	for from, to := range st.Promotions {
		found := false
		for _, t := range cfg.Tunnels {
			if t.ID == to {
				found = true
				break
			}
		}
		if found {
			if err := s.renameRecords(from, to); err != nil {
				log.Printf("promotion recovery: %v", err)
				continue
			}
			remapRuntime(&st, from, to)
		} else {
			delete(st.Promotions, from)
		}
	}
	if err := s.commitRuntime(st); err != nil {
		log.Printf("promotion recovery: %v", err)
	}
}

func (s *Engine) recoverRuntime() error {
	// Existing Unix children remain represented as orphans (never spawn a second
	// process). Windows job objects leave no survivors; saved leases stay stopped.
	st := s.runtimeSnapshot()
	for id := range st.Owners {
		if !s.hasOrphan(id) {
			delete(st.Owners, id)
		}
	}
	for i := range st.Leases {
		l := &st.Leases[i]
		if l.Revoked || l.Detached {
			continue
		}
		// No connection survives a daemon restart. Preserve an earlier disconnect
		// deadline rather than granting repeated restarts an unlimited extension.
		until := s.clock.Now().Add(DisconnectGrace)
		if l.GraceUntil == nil || until.Before(*l.GraceUntil) {
			l.GraceUntil = &until
		}
	}
	if err := s.commitRuntime(st); err != nil {
		return err
	}
	return s.ExpireLeases()
}

func parsePortRange(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid port range %q", value)
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	if errA != nil || errB != nil || a < 1 || b > 65535 || b < a {
		return 0, 0, fmt.Errorf("invalid port range %q", value)
	}
	return a, b, nil
}

func listeningLoopback(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.Contains(port, "/") {
		return false
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (s *Engine) allocatePorts(t *model.Tunnel, retry []int) ([]int, error) {
	used := map[int]bool{}
	for port := range s.portHistory[t.ID] {
		used[port] = true
	}
	external := map[int]bool{}
	concrete := map[int]int{}
	for _, other := range s.manager.Config().Tunnels {
		for _, f := range other.Forwards {
			if f.Kind == model.ForwardRemote {
				continue
			}
			_, port, err := net.SplitHostPort(f.Bind)
			if err != nil {
				port = f.Bind
			}
			if n, err := strconv.Atoi(port); err == nil {
				used[n] = true
				external[n] = true
			}
		}
	}
	// Reserve concrete forwards in this request even if they follow an auto
	// forward. Retried concrete binds remain reserved until the new spawn.
	for _, f := range t.Forwards {
		if f.Kind == model.ForwardRemote {
			continue
		}
		bind := f.Bind
		if f.ListenPort != "" && f.ListenPort != "auto" {
			bind = f.ListenPort
		}
		_, port, err := net.SplitHostPort(bind)
		if err != nil {
			port = bind
		}
		if n, err := strconv.Atoi(port); err == nil {
			used[n] = true
			concrete[n]++
		}
	}
	auto := []int{}
	for i := range t.Forwards {
		f := &t.Forwards[i]
		request := f.ListenPort
		for _, index := range retry {
			if i == index {
				request = "auto"
			}
		}
		if request == "" {
			if f.Kind != model.ForwardRemote {
				_, port, err := net.SplitHostPort(f.Bind)
				if err != nil {
					port = f.Bind
				}
				if n, err := strconv.Atoi(port); err == nil {
					used[n] = true
				}
			}
			continue
		}
		if t.Type != model.TypeSSH || (f.Kind != model.ForwardLocal && f.Kind != model.ForwardDynamic) {
			return nil, errors.New("listen_port requires a local or dynamic SSH forward")
		}
		if f.Bind != "" && request == f.ListenPort {
			return nil, errors.New("use either bind or listen_port, not both")
		}
		port := 0
		if request == "auto" {
			for n := s.portFirst; n <= s.portLast; n++ {
				address := net.JoinHostPort("127.0.0.1", strconv.Itoa(n))
				if !used[n] && !s.listening(address) {
					port = n
					break
				}
			}
			if port == 0 {
				return nil, errors.New("no free port in configured port range")
			}
			auto = append(auto, i)
			if s.portHistory[t.ID] == nil {
				s.portHistory[t.ID] = map[int]bool{}
			}
			s.portHistory[t.ID][port] = true
		} else {
			n, err := strconv.Atoi(request)
			if err != nil || n < 1 || n > 65535 {
				return nil, errors.New("listen_port must be auto or a decimal port")
			}
			if external[n] || concrete[n] > 1 || s.listening(net.JoinHostPort("127.0.0.1", strconv.Itoa(n))) {
				return nil, fmt.Errorf("port %d is already in use", n)
			}
			port = n
		}
		used[port] = true
		f.Bind = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		f.ListenPort = ""
	}
	return auto, nil
}

func (s *Engine) retryForward(ctx context.Context, t model.Tunnel, failure error) (model.Tunnel, bool, error) {
	// Stop holds configMu while waiting for the supervisor. Cancellation-aware
	// acquisition prevents a stop/retry lock cycle.
	for !s.configMu.TryLock() {
		select {
		case <-ctx.Done():
			return t, false, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer s.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return t, false, err
	}
	if err := s.lifetimeError(); err != nil {
		return t, false, err
	}
	st := s.runtimeSnapshot()
	indexes := st.AutoPorts[t.ID]
	if len(indexes) == 0 {
		return t, false, nil
	}
	found := -1
	for i, candidate := range st.Ephemeral {
		if candidate.ID == t.ID {
			found = i
			t = candidate
			break
		}
	}
	if found < 0 {
		return t, false, nil
	}
	t.Forwards = append([]model.Forward(nil), t.Forwards...)
	if _, err := s.allocatePorts(&t, indexes); err != nil {
		return t, false, err
	}
	st.Ephemeral[found] = t
	if err := s.commitRuntime(st); err != nil {
		return t, false, err
	}
	cfg := s.savedConfig()
	if err := s.applyWhileAlive(func() { s.setManagerConfig(s.merged(cfg)) }); err != nil {
		return t, false, err
	}
	return t, true, nil
}
