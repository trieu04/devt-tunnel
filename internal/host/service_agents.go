package host

import (
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
)

const eventApprovals = "approvals:changed"

func (s *Service) ListApprovals() ([]api.Approval, error) {
	return call[[]api.Approval](s, "approval.list", api.Empty{})
}

func (s *Service) DecideApproval(params api.DecideApprovalParams) (api.Approval, error) {
	approval, err := call[api.Approval](s, "approval.decide", params)
	if err == nil && params.Decision == "approve" && params.Scope == "always" {
		s.emitSettingsChanged()
	}
	return approval, err
}

func (s *Service) AgentSettings() (settings.Agent, error) {
	return call[settings.Agent](s, "settings.getAgent", api.Empty{})
}

func (s *Service) SetAgentSettings(agent settings.Agent) error {
	if err := action(s, "settings.setAgent", api.AgentSettingsParams{Agent: agent}); err != nil {
		return err
	}
	s.emitSettingsChanged()
	return nil
}

func (s *Service) ListLeases() ([]api.Lease, error) {
	return call[[]api.Lease](s, "lease.list", api.Empty{})
}

func (s *Service) RevokeLease(id string) error {
	return action(s, "lease.revoke", api.IDParams{ID: id})
}

func (s *Service) PromoteTunnel(id, name string) (string, error) {
	return call[string](s, "tunnel.promote", api.PromoteTunnelParams{ID: id, Name: name})
}

// ShowApprovals brings the native main window forward when a recovered pending
// request is first seen. It is harmless in browser/server mode.
func (s *Service) ShowApprovals() {
	s.mu.Lock()
	desktop := s.host
	s.mu.Unlock()
	if desktop != nil {
		desktop.showMain()
	}
}
