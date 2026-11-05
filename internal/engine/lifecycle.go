package engine

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

// Subscribe delivers events outside the engine lock; callbacks should return promptly.
func (s *Engine) Subscribe(callback func(api.Event)) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || callback == nil {
		return func() {}
	}
	s.nextSubscriber++
	id := s.nextSubscriber
	s.subscribers[id] = callback
	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

func (s *Engine) emit(event api.Event) {
	s.mu.Lock()
	callbacks := make([]func(api.Event), 0, len(s.subscribers))
	if !s.closed {
		for _, callback := range s.subscribers {
			callbacks = append(callbacks, callback)
		}
	}
	s.mu.Unlock()
	for _, callback := range callbacks {
		callback(event)
	}
}

func (s *Engine) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyTimer != nil || s.closed {
		return
	}
	s.notifyTimer = time.AfterFunc(notifyDelay, func() {
		s.mu.Lock()
		s.notifyTimer = nil
		s.mu.Unlock()
		s.emit(api.Event{Type: api.EventTunnelsChanged})
	})
}

func (s *Engine) Shutdown() {
	s.shutdown.Do(func() {
		s.lifecycleMu.Lock()
		s.mu.Lock()
		s.closed = true
		close(s.done)
		if s.notifyTimer != nil {
			s.notifyTimer.Stop()
			s.notifyTimer = nil
		}
		s.subscribers = nil
		s.manager.CloseAdmission()
		s.mu.Unlock()
		s.lifecycleMu.Unlock()
		s.broker.Close()
		s.manager.Shutdown()
		s.mu.Lock()
		orphans := append([]supervisor.Record(nil), s.orphans...)
		s.mu.Unlock()
		for _, orphan := range orphans {
			if err := s.StopOrphan(orphan.PID); err != nil {
				log.Printf("shutdown orphan %d: %v", orphan.PID, err)
			}
		}
		snapshot := s.runtimeSnapshot()
		for _, lease := range snapshot.Leases {
			if !lease.Revoked {
				if err := s.recordAudit(audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.LeaseRevoke, TunnelID: lease.TunnelID, LeaseID: lease.ID, Result: audit.ResultRevoked}); err != nil {
					log.Printf("audit shutdown: %v", err)
				}
			}
		}
		for _, tunnel := range snapshot.Ephemeral {
			result := audit.ResultOK
			if s.hasOrphan(tunnel.ID) {
				result = audit.ResultFailed
			}
			if err := s.recordAudit(audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.TunnelStopEphemeral, TunnelID: tunnel.ID, Result: result}); err != nil {
				log.Printf("audit shutdown: %v", err)
			}
		}
		if err := s.flushDenials("", true); err != nil {
			log.Printf("audit denial summary on shutdown: %v", err)
		}
		s.clearRuntimeOnShutdown()
	})
}

func (s *Engine) Active() bool {
	s.promptMu.Lock()
	pending := len(s.prompts) > 0
	s.promptMu.Unlock()
	if pending {
		return true
	}
	s.runtimeMu.RLock()
	owned := len(s.runtime.Ephemeral) > 0
	for _, l := range s.runtime.Leases {
		owned = owned || !l.Revoked
	}
	s.runtimeMu.RUnlock()
	return owned || s.orphanCount.Load() > 0 || s.manager.Active()
}

func (s *Engine) Wake() { s.manager.Wake() }

func (s *Engine) ForgetSecrets() { s.secrets.Forget() }

func (s *Engine) Statuses() []supervisor.Status {
	out := s.manager.RuntimeStatuses()
	for i := range out {
		out[i].Owner = s.owner(out[i].ID)
	}
	return out
}

func (s *Engine) Focus() {
	s.secrets.Forget()
	if changed, err := s.configStore.Changed(); err == nil && changed {
		s.loadConfig()
	}
}

// StartOnLaunch is requested by the GUI; constructing an engine never starts tunnels.
func (s *Engine) StartOnLaunch() {
	for _, tunnel := range s.manager.Config().Tunnels {
		if tunnel.StartOnLaunch && !strings.HasPrefix(tunnel.ID, "tmp-") {
			if err := s.Start(tunnel.ID); err != nil {
				log.Printf("start %s: %v", tunnel.ID, err)
			}
		}
	}
}

func (s *Engine) LogsSince(id string, sequence uint64) (uint64, []supervisor.LogLine) {
	return s.manager.LogsSince(id, sequence)
}

func (s *Engine) WaitReady(ctx context.Context, id string) (supervisor.Status, error) {
	return s.manager.WaitReady(ctx, id)
}

func (s *Engine) RecordUsed(id string) { s.recordUsed(id) }

func (s *Engine) monitorClockJump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	previous := time.Now().Round(0)
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			wall := now.Round(0)
			if clockJumped(previous, wall, 30*time.Second) {
				s.Wake()
			}
			previous = wall
		}
	}
}

func clockJumped(previous, current time.Time, interval time.Duration) bool {
	return current.Sub(previous) > interval+60*time.Second
}

func (s *Engine) lifetimeError() error {
	select {
	case <-s.done:
		return context.Canceled
	default:
		return nil
	}
}

func (s *Engine) applyWhileAlive(apply func()) error {
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	apply()
	return nil
}
