package engine

import (
	"errors"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
)

const MaxDenialAudits = 16
const denialInterval = time.Minute

type denialWindow struct {
	actor      api.Actor
	start      time.Time
	written    int
	suppressed uint64
}

// PolicyRefusalForSession coalesces refusals at the same boundary that binds
// the actor to its connection. Suppression affects auditing only, never policy.
func (s *Engine) PolicyRefusalForSession(actor api.Actor, session string) error {
	return s.denySession(actor, session, "", "")
}

func (s *Engine) denySession(actor api.Actor, session, id, reason string) error {
	if actor.Kind != "mcp" || session == "" {
		return s.policyDenied(actor, id)
	}
	s.denialMu.Lock()
	defer s.denialMu.Unlock()
	if s.denials == nil {
		s.denials = map[string]*denialWindow{}
	}
	w := s.denials[session]
	now := s.clock.Now()
	if w == nil {
		w = &denialWindow{actor: actor, start: now}
		s.denials[session] = w
	} else if !now.Before(w.start.Add(denialInterval)) {
		if err := s.flushDenialLocked(w); err != nil {
			return err
		}
		*w = denialWindow{actor: actor, start: now}
	}
	if w.written < MaxDenialAudits {
		if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.PolicyDeny, TunnelID: id, Decision: audit.Deny, Result: audit.ResultDenied}); err != nil {
			return err
		}
		w.written++
	} else {
		w.suppressed++
	}
	if reason == "" {
		reason = "agent policy denied this operation"
	}
	return api.NewError(api.PolicyDenied, reason)
}

func (s *Engine) flushDenialLocked(w *denialWindow) error {
	if w.suppressed == 0 {
		return nil
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(w.actor), Action: audit.PolicyDeny, Count: w.suppressed, Decision: audit.Deny, Result: audit.ResultDenied}); err != nil {
		return err
	}
	w.suppressed = 0
	return nil
}

// Maintenance ends elapsed windows even if no new call arrives. Disconnect
// flushes the partial window and drops its state. Failed appends stay pending.
func (s *Engine) flushDenials(session string, disconnect bool) error {
	s.denialMu.Lock()
	defer s.denialMu.Unlock()
	var out error
	for key, w := range s.denials {
		if session != "" && key != session {
			continue
		}
		if !disconnect && s.clock.Now().Before(w.start.Add(denialInterval)) {
			continue
		}
		if err := s.flushDenialLocked(w); err != nil {
			out = errors.Join(out, err)
			continue
		}
		delete(s.denials, key)
	}
	return out
}
