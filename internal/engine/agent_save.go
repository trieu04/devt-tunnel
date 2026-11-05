package engine

import (
	"strings"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/policy"
)

// SaveTunnelFor permits MCP only to add a new, fingerprint-approved definition.
// Editing and starting saved definitions remain separate person-only operations.
func (s *Engine) SaveTunnelFor(actor api.Actor, session string, p api.SaveTunnelParams) (string, error) {
	if actor.Kind != "mcp" {
		return s.saveTunnel(p.Tunnel, p.Start, func(id string) error {
			return s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelSave, TunnelID: id, Result: audit.ResultOK})
		})
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.validSession(actor, session); err != nil {
		return "", err
	}
	if p.Tunnel.ID != "" || p.Start {
		return "", s.denySession(actor, session, p.Tunnel.ID, "")
	}
	t := p.Tunnel
	if err := s.agentBuilt(actor, session, t); err != nil {
		return "", err
	}
	key := requestKey(struct {
		Method string
		Tunnel model.Tunnel
	}{"tunnel.save", t})
	fingerprint, err := s.authorize(actor, session, policy.ActionSave, t, p.Purpose, key)
	if err != nil {
		return "", err
	}
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		taken := map[string]bool{}
		for _, candidate := range s.manager.Config().Tunnels {
			taken[candidate.ID] = true
		}
		t.ID = model.NewID(t.Name, taken)
		if strings.HasPrefix(t.ID, "tmp-") {
			t.ID = model.NewID("saved-"+t.Name, taken)
		}
		cfg.Tunnels = append(cfg.Tunnels, t)
		if err := s.merged(*cfg).Validate(); err != nil {
			return err
		}
		return s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.TunnelSave, TunnelID: t.ID, Fingerprint: fingerprint, Result: audit.ResultOK})
	})
	if err != nil {
		return "", err
	}
	if err := s.applyWhileAlive(func() { s.setManagerConfig(s.merged(cfg)) }); err != nil {
		return "", err
	}
	return t.ID, nil
}
