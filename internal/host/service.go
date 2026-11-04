// Package host connects the engine to Wails and desktop integration.
package host

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const (
	eventTunnels = "tunnels:changed"
	eventPrompt  = "askpass:prompt"
)

var Version = "0.1.0"

type Service struct {
	engine         *engine.Engine
	auth           platform.UserAuth
	keychain       secret.Store
	host           *desktop
	mu             sync.Mutex
	shortcut       string
	shortcutErr    string
	unsubscribe    func()
	logWatchCancel context.CancelFunc
	updateChecked  time.Time
	updateInfo     UpdateInfo
}

func NewService() (*Service, error) {
	keychain := platform.Keychain{}
	core, err := engine.New(engine.Options{Version: Version, Secrets: keychain})
	if err != nil {
		return nil, err
	}
	s := &Service{engine: core, auth: platform.NewUserAuth(), keychain: keychain}
	s.unsubscribe = core.Subscribe(func(event api.Event) {
		s.mu.Lock()
		desktop := s.host
		s.mu.Unlock()
		if desktop == nil {
			return
		}
		switch event.Type {
		case api.EventTunnelsChanged:
			desktop.app.Event.Emit(eventTunnels)
		case api.EventPromptAdded:
			if event.Prompt != nil {
				desktop.app.Event.Emit(eventPrompt, *event.Prompt)
				desktop.showMain()
			}
		}
	})
	return s, nil
}

func (s *Service) Overview() api.Overview {
	overview := s.engine.Overview()
	s.mu.Lock()
	overview.Shortcut, overview.ShortcutError = s.shortcut, s.shortcutErr
	s.mu.Unlock()
	return overview
}

func (s *Service) RevealConfig() error {
	path, err := s.engine.EnsureConfigFile()
	if err != nil {
		return err
	}
	return s.host.app.Env.OpenFileManager(path, true)
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	if s.logWatchCancel != nil {
		s.logWatchCancel()
	}
	s.mu.Unlock()
	s.unsubscribe()
	s.engine.Shutdown()
}

func (s *Service) Start(id string) error { return s.engine.Start(id) }

func (s *Service) Stop(id string) { s.engine.Stop(id) }

func (s *Service) Toggle(id string) error { return s.engine.Toggle(id) }

func (s *Service) Logs(id string) []string { return s.engine.Logs(id) }

func (s *Service) Findings(id string) []checks.Finding { return s.engine.Findings(id) }

func (s *Service) Reload() { s.engine.Reload() }

func (s *Service) SetSecret(name, value string, kind secret.Kind) error {
	return s.engine.SetSecret(name, value, kind)
}

func (s *Service) StopOrphan(pid int) error { return s.engine.StopOrphan(pid) }

func (s *Service) AnswerPrompt(id, value string, cancelled bool) {
	s.engine.AnswerPrompt(id, value, cancelled)
}

func (s *Service) EvaluateDraft(t model.Tunnel) api.DraftResult { return s.engine.EvaluateDraft(t) }

func (s *Service) NewDraft(kind model.TunnelType) model.Tunnel { return s.engine.NewDraft(kind) }

func (s *Service) ImportCommand(line string) (importer.Result, error) {
	return s.engine.ImportCommand(line)
}

func (s *Service) DuplicateDraft(id string) (model.Tunnel, error) { return s.engine.DuplicateDraft(id) }

func (s *Service) SaveTunnel(t model.Tunnel, start bool) (string, error) {
	return s.engine.SaveTunnel(t, start)
}

func (s *Service) DeleteTunnel(id string) error { return s.engine.DeleteTunnel(id) }

func (s *Service) SetFavorite(id string, favorite bool) error {
	return s.engine.SetFavorite(id, favorite)
}

func (s *Service) Restart(id string) error { return s.engine.Restart(id) }

func (s *Service) ActivateProfile(id string) error { return s.engine.ActivateProfile(id) }

func (s *Service) SaveProfile(profile model.Profile, members []string) (string, error) {
	return s.engine.SaveProfile(profile, members)
}

func (s *Service) DeleteProfile(id string) error { return s.engine.DeleteProfile(id) }

func (s *Service) DuplicateProfile(id string) (string, error) { return s.engine.DuplicateProfile(id) }

func (s *Service) SetProfileAutostart(id, tunnelID string, on bool) error {
	return s.engine.SetProfileAutostart(id, tunnelID, on)
}

func (s *Service) StartProfile(id string) error { return s.engine.StartProfile(id) }

func (s *Service) StopProfile(id string) { s.engine.StopProfile(id) }

func (s *Service) SSHHosts(profileID string) api.SSHHostList { return s.engine.SSHHosts(profileID) }

func (s *Service) ResolveSSHHost(alias, profileID string) (sshconfig.Resolved, error) {
	return s.engine.ResolveSSHHost(alias, profileID)
}

func (s *Service) ReloadSSHHosts() { s.engine.ReloadSSHHosts() }

func (s *Service) Tools() []api.ToolInfo { return s.engine.Tools() }

func (s *Service) SetToolPath(tool, path string) ([]api.ToolInfo, error) {
	return s.engine.SetToolPath(tool, path)
}

func (s *Service) ListSecrets() []api.SecretView { return s.engine.ListSecrets() }

func (s *Service) DeleteSecret(name string) error { return s.engine.DeleteSecret(name) }

func (s *Service) RenameSecret(from, to string) error { return s.engine.RenameSecret(from, to) }

func (s *Service) AppVersion() string { return Version }

func (s *Service) ShowMain() {
	if s.host != nil {
		s.host.showMain()
	}
}

func (s *Service) HideQuickLaunch() {
	if s.host != nil {
		s.host.quick.Hide()
	}
}

func (s *Service) ShowQuickLaunch() {
	if s.host != nil {
		s.host.showQuick()
	}
}

func (s *Service) Quit() {
	if s.host != nil {
		s.host.app.Quit()
	}
}

func (s *Service) WatchLogs(id string) []supervisor.LogLine {
	s.mu.Lock()
	if s.logWatchCancel != nil {
		s.logWatchCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.logWatchCancel = cancel
	s.mu.Unlock()
	if id == "" || s.host == nil {
		return []supervisor.LogLine{}
	}
	_, initial := s.engine.LogsSince(id, 0)
	var seq uint64
	if len(initial) > 0 {
		seq = initial[len(initial)-1].Seq
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next, lines := s.engine.LogsSince(id, seq)
				seq = next
				if len(lines) > 0 && s.host != nil {
					s.host.app.Event.Emit("logs:appended", api.LogAppend{ID: id, Lines: lines})
				}
			}
		}
	}()
	return initial
}

func (s *Service) OpenIn(screen, param string) {
	if s.host != nil {
		s.host.app.Event.Emit("nav:open", map[string]string{"screen": screen, "param": param})
		s.ShowMain()
	}
}

func (s *Service) StartAndCopy(id string) error {
	if err := s.engine.Start(id); err != nil {
		return err
	}
	s.engine.RecordUsed(id)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		status, err := s.engine.WaitReady(ctx, id)
		if err == nil && status.Address != "" && s.host != nil {
			_ = s.host.app.Clipboard.SetText(status.Address)
		}
	}()
	return nil
}

func (s *Service) RevealSecret(name string) (string, error) {
	if !s.auth.Available() {
		return "", errors.New("revealing secrets is unavailable without OS user authentication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := s.auth.Confirm(ctx, "Reveal secret "+name); err != nil {
		return "", err
	}
	return s.keychain.Get(name)
}

func (s *Service) UserAuthAvailable() bool { return s.auth.Available() }
