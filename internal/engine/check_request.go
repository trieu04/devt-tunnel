package engine

import (
	"fmt"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/policy"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
)

func cloneRequestTunnel(t model.Tunnel) model.Tunnel {
	return corestate.Runtime{Ephemeral: []model.Tunnel{t}}.Clone().Ephemeral[0]
}

// AgentPolicy exposes a fixed, explicitly non-sensitive policy view. The port
// range reflects an embedding override too, rather than an unused setting.
func (s *Engine) AgentPolicy() api.AgentPolicyResult {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	a := s.agentSettings().AgentPolicy()
	return api.AgentPolicyResult{
		Enabled: a.Enabled, Public: a.Public,
		AllowTunnels: append([]string{}, a.AllowTunnels...), AllowSSHHosts: append([]string{}, a.AllowSSHHosts...),
		PortRange: fmt.Sprintf("%d-%d", s.portFirst, s.portLast), DefaultTTL: a.DefaultTTL, MaxTTL: a.MaxTTL,
	}
}

// CheckRequest diagnoses an open, not ActionCheck's unconditional read grant.
// It reads daemon-loaded saved definitions and true built SSH alias facts, but
// does not create/consume approvals, reserve ports, audit, or touch keychains.
func (s *Engine) CheckRequest(actor api.Actor, p api.OpenRequestParams) (api.CheckRequestResult, error) {
	if err := validateOpenRequest(p, false); err != nil {
		return api.CheckRequestResult{}, err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return api.CheckRequestResult{}, err
	}
	t := model.Tunnel{}
	saved := p.ID != ""
	if saved {
		for _, candidate := range s.savedConfig().Tunnels {
			if candidate.ID == p.ID {
				t = candidate
				break
			}
		}
		if t.ID == "" {
			if s.known(p.ID) {
				return api.CheckRequestResult{Decision: "deny", Reason: "temporary tunnels are reused by requestId, not saved id", Findings: []checks.Finding{}}, nil
			}
			return api.CheckRequestResult{}, api.NewError(api.Unknown, "saved tunnel not found")
		}
	} else {
		t = cloneRequestTunnel(*p.Tunnel)
		if t.ID == "" {
			t.ID = "tmp-check"
		}
	}
	result := api.CheckRequestResult{Findings: []checks.Finding{}}
	deny := func(reason string) { result.Decision, result.Reason = string(policy.Deny), reason }
	if actor.Kind == "mcp" && !saved {
		if reason := s.agentBuiltReason(t); reason != "" {
			deny(reason)
			result.Findings = append([]checks.Finding{}, checks.AgentSafe(t)...)
			return result, nil
		}
	}
	if !saved {
		if !strings.HasPrefix(t.ID, "tmp-") {
			deny("ephemeral ids must start with tmp-")
			return result, nil
		}
		if _, err := s.allocatePortsFor(&t, nil, false); err != nil {
			deny(err.Error())
			return result, nil
		}
		if s.known(t.ID) {
			deny("duplicate tunnel id")
			return result, nil
		}
		cfg := s.manager.Config()
		cfg.Tunnels = append(append([]model.Tunnel{}, cfg.Tunnels...), t)
		if err := cfg.Validate(); err != nil {
			deny(err.Error())
			return result, nil
		}
	}
	draft := s.EvaluateDraftFor(actor, t)
	result.Preview, result.Findings, result.BuildError = draft.Preview, draft.Findings, draft.BuildError
	// Saved execution/credential fields must not reappear through this endpoint.
	if actor.Kind == "mcp" && saved {
		result.Preview = ""
		if result.BuildError != "" {
			result.BuildError = "saved tunnel could not be built"
		}
	}
	if draft.BuildError != "" {
		deny("tunnel could not be built")
		return result, nil
	}
	if _, err := s.leaseTTL(actor, p.TTL); err != nil {
		deny(err.Error())
		return result, nil
	}
	action := policy.ActionCreate
	if saved {
		action = policy.ActionOpenSaved
	} else if policy.PublicExposure(t) {
		action = policy.ActionExpose
	}
	cfg := s.agentSettings()
	decision, reason := policy.Decide(actor, action, t, cfg)
	if decision != policy.Deny {
		spec, err := s.manager.BuildDraft(t)
		if err != nil {
			deny("tunnel could not be built")
			return result, nil
		}
		decision, reason, _ = s.builtPolicyDecision(actor, action, t, cfg, spec)
	}
	result.Decision, result.Reason = string(decision), reason
	return result, nil
}
