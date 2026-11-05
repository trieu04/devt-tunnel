package control

import (
	"context"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func action[P any](s *Server, method string, fn func(P) error) {
	handle(s, method, func(_ context.Context, p P) (api.Empty, error) { return api.Empty{}, fn(p) })
}
func read[P, R any](s *Server, method string, fn func(context.Context, P) (R, error)) {
	handle(s, method, fn)
	s.readOnly[method] = true
}
func query[P, R any](s *Server, method string, fn func(P) R) {
	read(s, method, func(_ context.Context, p P) (R, error) { return fn(p), nil })
}

func (s *Server) register() {
	e := s.engine
	handle(s, "tunnel.promote", func(ctx context.Context, p api.PromoteTunnelParams) (string, error) {
		actor, session := connectionActor(ctx)
		return e.PromoteTunnelFor(actor, session, p)
	})
	read(s, "approval.list", func(ctx context.Context, _ api.Empty) ([]api.Approval, error) {
		actor, session := connectionActor(ctx)
		return e.ListApprovals(actor, session)
	})
	read(s, "approval.get", func(ctx context.Context, p api.IDParams) (api.Approval, error) {
		actor, session := connectionActor(ctx)
		return e.GetApproval(actor, session, p.ID)
	})
	handle(s, "approval.decide", func(ctx context.Context, p api.DecideApprovalParams) (api.Approval, error) {
		actor, _ := connectionActor(ctx)
		return e.DecideApproval(actor, p)
	})
	read(s, "audit.read", func(_ context.Context, p api.AuditReadParams) (api.AuditReadResult, error) { return e.ReadAudit(p) })
	handle(s, "tunnel.runEphemeral", func(ctx context.Context, p api.RunEphemeralParams) (api.RunEphemeralResult, error) {
		actor, session := connectionActor(ctx)
		return e.RunEphemeralContext(ctx, actor, session, p)
	})
	handle(s, "lease.open", func(ctx context.Context, p api.OpenLeaseParams) (api.Lease, error) {
		actor, session := connectionActor(ctx)
		return e.OpenLeaseContext(ctx, actor, session, p)
	})
	handle(s, "lease.renew", func(ctx context.Context, p api.RenewLeaseParams) (api.Lease, error) {
		actor, session := connectionActor(ctx)
		return e.RenewLease(actor, session, p)
	})
	handle(s, "lease.close", func(ctx context.Context, p api.IDParams) (api.Empty, error) {
		actor, session := connectionActor(ctx)
		return api.Empty{}, e.CloseLease(actor, session, p.ID)
	})
	read(s, "lease.list", func(ctx context.Context, _ api.Empty) ([]api.Lease, error) {
		actor, session := connectionActor(ctx)
		return e.ListLeases(actor, session, actor.Kind == "mcp"), nil
	})
	handle(s, "lease.revoke", func(ctx context.Context, p api.IDParams) (api.Empty, error) {
		actor, _ := connectionActor(ctx)
		return api.Empty{}, e.RevokeLeaseFor(actor, p.ID)
	})
	query(s, api.DaemonHello, func(api.HelloParams) api.HelloResult { return s.Hello() })
	read(s, api.DaemonStatusMethod, func(ctx context.Context, _ api.Empty) (api.DaemonStatus, error) {
		statuses := e.Statuses()
		actor, _ := connectionActor(ctx)
		if actor.Kind == "mcp" {
			for i := range statuses {
				statuses[i] = mcpStatus(statuses[i])
			}
		}
		return api.DaemonStatus{HelloResult: s.Hello(), Active: e.Active(), Connections: s.Connections(), Tunnels: statuses}, nil
	})
	// These methods need connection state or must reply before the process exits.
	handle(s, api.DaemonShutdown, func(context.Context, api.ShutdownParams) (api.Empty, error) { return api.Empty{}, nil })
	query(s, api.EventsSubscribe, func(api.Empty) api.Empty { return api.Empty{} })
	read(s, "tunnel.list", func(ctx context.Context, _ api.Empty) ([]api.TunnelView, error) {
		views := e.Overview().Tunnels
		actor, _ := connectionActor(ctx)
		if actor.Kind == "mcp" {
			for i := range views {
				views[i].Status = mcpStatus(views[i].Status)
			}
		}
		return views, nil
	})
	read(s, "tunnel.get", func(ctx context.Context, p api.IDParams) (api.TunnelView, error) {
		for _, v := range e.Overview().Tunnels {
			if v.Tunnel.ID == p.ID {
				actor, _ := connectionActor(ctx)
				if actor.Kind == "mcp" {
					v.Status = mcpStatus(v.Status)
				}
				return v, nil
			}
		}
		return api.TunnelView{}, api.NewError(api.Unknown, "unknown tunnel")
	})
	handle(s, "tunnel.save", func(ctx context.Context, p api.SaveTunnelParams) (string, error) {
		actor, session := connectionActor(ctx)
		return e.SaveTunnelFor(actor, session, p)
	})
	action(s, "tunnel.delete", func(p api.IDParams) error { return e.DeleteTunnel(p.ID) })
	action(s, "tunnel.start", func(p api.IDParams) error { return e.Start(p.ID) })
	handle(s, "tunnel.stop", func(ctx context.Context, p api.IDParams) (api.Empty, error) {
		actor, _ := connectionActor(ctx)
		return api.Empty{}, e.StopTunnelFor(actor, p.ID)
	})
	action(s, "tunnel.restart", func(p api.IDParams) error { return e.Restart(p.ID) })
	read(s, "tunnel.waitReady", func(ctx context.Context, p api.IDParams) (supervisor.Status, error) {
		status, err := e.WaitReady(ctx, p.ID)
		actor, _ := connectionActor(ctx)
		if actor.Kind == "mcp" {
			status = mcpStatus(status)
			if err != nil {
				rpc := wireError(err)
				rpc.Message = "tunnel did not become ready; inspect redacted logs"
				err = rpc
			}
		}
		return status, err
	})
	query(s, "tunnel.evaluateDraft", func(p api.TunnelParams) api.DraftResult { return e.EvaluateDraft(p.Tunnel) })
	read(s, "tunnel.import", func(_ context.Context, p api.ImportParams) (importer.Result, error) { return e.ImportCommand(p.Line) })
	query(s, "tunnel.newDraft", func(p api.NewDraftParams) model.Tunnel { return e.NewDraft(p.Kind) })
	read(s, "tunnel.duplicateDraft", func(_ context.Context, p api.IDParams) (model.Tunnel, error) { return e.DuplicateDraft(p.ID) })
	action(s, "tunnel.setFavorite", func(p api.FavoriteParams) error { return e.SetFavorite(p.ID, p.Favorite) })
	query(s, "tunnel.findings", func(p api.IDParams) []checks.Finding { return e.Findings(p.ID) })
	action(s, "tunnel.recordUsed", func(p api.IDParams) error { e.RecordUsed(p.ID); return nil })
	action(s, "tunnel.startOnLaunch", func(api.Empty) error { e.StartOnLaunch(); return nil })
	read(s, "logs.read", func(ctx context.Context, p api.IDParams) ([]string, error) {
		c := ctx.Value(connectionKey{}).(*connection)
		if c.client.Kind != "mcp" {
			return e.Logs(p.ID), nil
		}
		result, err := e.RedactedLogsSince(c.logs, p.ID, 0)
		out := []string{}
		for _, line := range result.Lines {
			out = append(out, line.Line)
		}
		return out, err
	})
	read(s, "logs.since", func(ctx context.Context, p api.LogsParams) (api.LogsResult, error) {
		c := ctx.Value(connectionKey{}).(*connection)
		if c.client.Kind == "mcp" {
			return e.RedactedLogsSince(c.logs, p.ID, p.Sequence)
		}
		sequence, lines := e.LogsSince(p.ID, p.Sequence)
		return api.LogsResult{Sequence: sequence, Lines: lines}, nil
	})
	query(s, "profile.list", func(api.Empty) []model.Profile { return e.Overview().Profiles })
	handle(s, "profile.save", func(_ context.Context, p api.SaveProfileParams) (string, error) {
		return e.SaveProfile(p.Profile, p.Members)
	})
	action(s, "profile.delete", func(p api.IDParams) error { return e.DeleteProfile(p.ID) })
	handle(s, "profile.duplicate", func(_ context.Context, p api.IDParams) (string, error) { return e.DuplicateProfile(p.ID) })
	action(s, "profile.activate", func(p api.IDParams) error { return e.ActivateProfile(p.ID) })
	action(s, "profile.setAutostart", func(p api.AutostartParams) error { return e.SetProfileAutostart(p.ID, p.TunnelID, p.On) })
	action(s, "profile.start", func(p api.IDParams) error { return e.StartProfile(p.ID) })
	action(s, "profile.stop", func(p api.IDParams) error { e.StopProfile(p.ID); return nil })
	query(s, "secret.list", func(api.Empty) []api.SecretView { return e.ListSecrets() })
	action(s, "secret.set", func(p api.SetSecretParams) error { return e.SetSecret(p.Name, p.Value, p.Kind) })
	action(s, "secret.delete", func(p api.SecretNameParams) error { return e.DeleteSecret(p.Name) })
	action(s, "secret.rename", func(p api.RenameSecretParams) error { return e.RenameSecret(p.From, p.To) })
	action(s, "secret.forget", func(api.Empty) error { e.ForgetSecrets(); return nil })
	read(s, "prompt.list", func(ctx context.Context, _ api.Empty) ([]api.PendingPrompt, error) {
		prompts := e.Overview().Prompts
		actor, _ := connectionActor(ctx)
		if actor.Kind == "mcp" {
			for i := range prompts {
				prompts[i].Prompt.Text = ""
			}
		}
		return prompts, nil
	})
	action(s, "prompt.answer", func(p api.AnswerPromptParams) error { e.AnswerPrompt(p.ID, p.Value, p.Cancelled); return nil })
	query(s, "ssh.hosts", func(p api.SSHParams) api.SSHHostList { return e.SSHHosts(p.ProfileID) })
	read(s, "ssh.resolve", func(ctx context.Context, p api.SSHParams) (sshconfig.Resolved, error) {
		actor, _ := connectionActor(ctx)
		if actor.Kind == "mcp" {
			t := model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: p.Alias}}
			if p.Alias == "" || len(checks.AgentSafe(t)) > 0 {
				return sshconfig.Resolved{}, e.PolicyRefusal(actor)
			}
		}
		return e.ResolveSSHHost(p.Alias, p.ProfileID)
	})
	action(s, "ssh.reload", func(api.Empty) error { e.ReloadSSHHosts(); return nil })
	query(s, "tool.list", func(api.Empty) []api.ToolInfo { return e.Tools() })
	handle(s, "tool.setPath", func(_ context.Context, p api.ToolPathParams) ([]api.ToolInfo, error) {
		return e.SetToolPath(p.Tool, p.Path)
	})
	query(s, "settings.get", func(api.Empty) api.SettingsResult {
		return api.SettingsResult{Shortcut: e.Shortcut(), UpdateChecks: e.UpdateChecksEnabled()}
	})
	query(s, "settings.getAgent", func(api.Empty) settings.Agent { return e.AgentSettings() })
	handle(s, "settings.setAgent", func(ctx context.Context, p api.AgentSettingsParams) (api.Empty, error) {
		actor, _ := connectionActor(ctx)
		return api.Empty{}, e.SetAgentSettingsFor(actor, p.Agent)
	})
	action(s, "settings.setShortcut", func(p api.ShortcutParams) error { return e.SetShortcut(p.Shortcut) })
	action(s, "settings.setUpdateChecks", func(p api.UpdateChecksParams) error { return e.SetUpdateChecks(p.Enabled) })
	query(s, "orphan.list", func(api.Empty) []supervisor.Record { return e.Overview().Orphans })
	action(s, "orphan.stop", func(p api.OrphanParams) error { return e.StopOrphan(p.PID) })
	query(s, "config.overview", func(api.Empty) api.Overview { return e.Overview() })
	handle(s, "config.path", func(context.Context, api.Empty) (string, error) { return e.EnsureConfigFile() })
	action(s, "config.reload", func(api.Empty) error { return e.Reload() })
	action(s, "system.wake", func(api.Empty) error { e.Wake(); return nil })
	action(s, "system.focus", func(api.Empty) error { e.Focus(); return nil })
}

// Status errors can be copied from process output. Do not let inspection or
// waitReady become a raw-log side channel around the streaming log redactor.
func mcpStatus(status supervisor.Status) supervisor.Status {
	if status.Error != "" {
		status.Error = "tunnel error; inspect redacted logs"
	}
	return status
}
