package control

import (
	"context"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
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
	query(s, api.DaemonHello, func(api.HelloParams) api.HelloResult { return s.Hello() })
	query(s, api.DaemonStatusMethod, func(api.Empty) api.DaemonStatus {
		return api.DaemonStatus{HelloResult: s.Hello(), Active: e.Active(), Connections: s.Connections(), Tunnels: e.Statuses()}
	})
	// These methods need connection state or must reply before the process exits.
	handle(s, api.DaemonShutdown, func(context.Context, api.ShutdownParams) (api.Empty, error) { return api.Empty{}, nil })
	query(s, api.EventsSubscribe, func(api.Empty) api.Empty { return api.Empty{} })
	query(s, "tunnel.list", func(api.Empty) []api.TunnelView { return e.Overview().Tunnels })
	read(s, "tunnel.get", func(_ context.Context, p api.IDParams) (api.TunnelView, error) {
		for _, v := range e.Overview().Tunnels {
			if v.Tunnel.ID == p.ID {
				return v, nil
			}
		}
		return api.TunnelView{}, api.NewError(api.Unknown, "unknown tunnel")
	})
	handle(s, "tunnel.save", func(_ context.Context, p api.SaveTunnelParams) (string, error) {
		return e.SaveTunnel(p.Tunnel, p.Start)
	})
	action(s, "tunnel.delete", func(p api.IDParams) error { return e.DeleteTunnel(p.ID) })
	action(s, "tunnel.start", func(p api.IDParams) error { return e.Start(p.ID) })
	action(s, "tunnel.stop", func(p api.IDParams) error { e.Stop(p.ID); return nil })
	action(s, "tunnel.restart", func(p api.IDParams) error { return e.Restart(p.ID) })
	read(s, "tunnel.waitReady", func(ctx context.Context, p api.IDParams) (supervisor.Status, error) { return e.WaitReady(ctx, p.ID) })
	query(s, "tunnel.evaluateDraft", func(p api.TunnelParams) api.DraftResult { return e.EvaluateDraft(p.Tunnel) })
	read(s, "tunnel.import", func(_ context.Context, p api.ImportParams) (importer.Result, error) { return e.ImportCommand(p.Line) })
	query(s, "tunnel.newDraft", func(p api.NewDraftParams) model.Tunnel { return e.NewDraft(p.Kind) })
	read(s, "tunnel.duplicateDraft", func(_ context.Context, p api.IDParams) (model.Tunnel, error) { return e.DuplicateDraft(p.ID) })
	action(s, "tunnel.setFavorite", func(p api.FavoriteParams) error { return e.SetFavorite(p.ID, p.Favorite) })
	query(s, "tunnel.findings", func(p api.IDParams) []checks.Finding { return e.Findings(p.ID) })
	action(s, "tunnel.recordUsed", func(p api.IDParams) error { e.RecordUsed(p.ID); return nil })
	action(s, "tunnel.startOnLaunch", func(api.Empty) error { e.StartOnLaunch(); return nil })
	query(s, "logs.read", func(p api.IDParams) []string { return e.Logs(p.ID) })
	query(s, "logs.since", func(p api.LogsParams) api.LogsResult {
		sequence, lines := e.LogsSince(p.ID, p.Sequence)
		return api.LogsResult{Sequence: sequence, Lines: lines}
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
	query(s, "prompt.list", func(api.Empty) []api.PendingPrompt { return e.Overview().Prompts })
	action(s, "prompt.answer", func(p api.AnswerPromptParams) error { e.AnswerPrompt(p.ID, p.Value, p.Cancelled); return nil })
	query(s, "ssh.hosts", func(p api.SSHParams) api.SSHHostList { return e.SSHHosts(p.ProfileID) })
	read(s, "ssh.resolve", func(_ context.Context, p api.SSHParams) (sshconfig.Resolved, error) {
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
