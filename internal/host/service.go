// Package host connects the daemon client to Wails and desktop integration.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const (
	eventTunnels  = "tunnels:changed"
	eventPrompt   = "askpass:prompt"
	eventSettings = "settings:changed"
)

const (
	connectTimeout  = 5 * time.Second
	callTimeout     = 30 * time.Second
	shutdownTimeout = 15 * time.Second
)

var Version = "0.1.0"

type daemonClient interface {
	Call(context.Context, string, any, any) error
	Events(func(api.Event))
	Done() <-chan struct{}
	Close() error
}

type connector func(context.Context) (daemonClient, error)

type Service struct {
	auth             platform.UserAuth
	keychain         secret.Store
	host             *desktop
	mu               sync.Mutex
	connectMu        sync.Mutex
	client           daemonClient
	clientGeneration uint64
	connectionErr    error
	overview         api.Overview
	settings         api.SettingsResult
	settingsLoaded   bool
	shortcut         string
	shortcutErr      string
	logWatchCancel   context.CancelFunc
	updateChecked    time.Time
	updateInfo       UpdateInfo
	updateClient     httpDoer
	connect          connector
	shutdownConnect  connector
	shutdownLimit    time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	shutdown         sync.Once
	eventSink        func(api.Event)
	logSink          func(api.LogAppend)
	settingsSink     func()
	ready            bool
	startRequested   bool
	startPending     bool
	startInFlight    bool
	stopping         bool
}

func NewService() (*Service, error) {
	keychain := platform.Keychain{}
	info := api.ClientInfo{Kind: "gui", Name: "devt-tunnel", Version: Version}
	connect := func(ctx context.Context) (daemonClient, error) {
		return client.Ensure(ctx, client.EnsureOptions{Info: info})
	}
	shutdownConnect := func(ctx context.Context) (daemonClient, error) {
		socket, err := platform.ControlSocket()
		if err != nil {
			return nil, err
		}
		return client.DialManagement(ctx, socket, info)
	}
	return newServiceWithConnectors(connect, shutdownConnect, platform.NewUserAuth(), keychain), nil
}

func newService(connect connector, auth platform.UserAuth, keychain secret.Store) *Service {
	return newServiceWithConnectors(connect, connect, auth, keychain)
}

func newServiceWithConnectors(connect, shutdownConnect connector, auth platform.UserAuth, keychain secret.Store) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		auth: auth, keychain: keychain, connect: connect, shutdownConnect: shutdownConnect,
		shutdownLimit: shutdownTimeout, ctx: ctx, cancel: cancel,
		settings: api.SettingsResult{UpdateChecks: true}, updateClient: &http.Client{Timeout: 5 * time.Second},
	}
	initialCtx, initialCancel := context.WithTimeout(ctx, connectTimeout)
	initial, err := s.connectClient(initialCtx)
	initialCancel()
	if err != nil {
		s.setConnectionError(err)
	}
	s.wg.Add(1)
	go s.maintainConnection(initial)
	return s
}

func (s *Service) connectClient(ctx context.Context) (daemonClient, error) {
	s.connectMu.Lock()
	c, err := s.connect(ctx)
	if err != nil {
		s.connectMu.Unlock()
		return nil, err
	}
	c.Events(s.handleEvent)
	var subscribed api.Empty
	if err = c.Call(ctx, api.EventsSubscribe, api.Empty{}, &subscribed); err != nil {
		c.Close()
		s.connectMu.Unlock()
		return nil, err
	}
	var overview api.Overview
	if err = c.Call(ctx, "config.overview", api.Empty{}, &overview); err != nil {
		c.Close()
		s.connectMu.Unlock()
		return nil, err
	}
	var settings api.SettingsResult
	if err = c.Call(ctx, "settings.get", api.Empty{}, &settings); err != nil {
		c.Close()
		s.connectMu.Unlock()
		return nil, err
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		c.Close()
		s.connectMu.Unlock()
		return nil, context.Canceled
	}
	s.client = c
	s.clientGeneration++
	s.connectionErr = nil
	s.overview = overview
	s.settings = settings
	s.settingsLoaded = true
	desktop := s.host
	ready := s.ready
	s.mu.Unlock()
	s.connectMu.Unlock()
	if desktop != nil {
		desktop.app.Event.Emit(eventTunnels)
		if ready {
			desktop.registerShortcut()
		}
	}
	s.emitSettingsChanged()
	s.flushStartMarked()
	return c, nil
}

func (s *Service) maintainConnection(current daemonClient) {
	defer s.wg.Done()
	backoff := 250 * time.Millisecond
	for {
		if current != nil {
			select {
			case <-s.ctx.Done():
				return
			case <-current.Done():
				s.markDisconnected(current, io.EOF)
				current = nil
				backoff = 250 * time.Millisecond
			}
			continue
		}
		timer := time.NewTimer(backoff)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		attemptCtx, cancel := context.WithTimeout(s.ctx, connectTimeout)
		connected, err := s.connectClient(attemptCtx)
		cancel()
		if err != nil {
			s.setConnectionError(err)
			backoff = min(backoff*2, 5*time.Second)
			continue
		}
		current = connected
		backoff = 250 * time.Millisecond
	}
}

func (s *Service) setConnectionError(err error) {
	if err == nil {
		err = io.EOF
	}
	s.mu.Lock()
	s.connectionErr = err
	desktop := s.host
	s.mu.Unlock()
	if desktop != nil {
		desktop.app.Event.Emit(eventTunnels)
	}
}

func (s *Service) markDisconnected(c daemonClient, err error) {
	s.mu.Lock()
	if s.client == c {
		s.client = nil
		s.connectionErr = err
	}
	desktop := s.host
	s.mu.Unlock()
	if desktop != nil {
		desktop.app.Event.Emit(eventTunnels)
	}
}

func (s *Service) handleEvent(event api.Event) {
	s.mu.Lock()
	desktop := s.host
	sink := s.eventSink
	s.mu.Unlock()
	if sink != nil {
		sink(event)
	}
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
}

func callContext[R any](ctx context.Context, s *Service, method string, params any) (R, error) {
	var result R
	s.mu.Lock()
	c := s.client
	err := s.connectionErr
	s.mu.Unlock()
	if c == nil {
		if err == nil {
			err = errors.New("connection is not ready")
		}
		return result, fmt.Errorf("Cannot reach the tunnel service: %w", err)
	}
	if err = c.Call(ctx, method, params, &result); err != nil {
		select {
		case <-c.Done():
			s.markDisconnected(c, err)
		default:
		}
	}
	return result, err
}

func call[R any](s *Service, method string, params any) (R, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return callContext[R](ctx, s, method, params)
}

func action(s *Service, method string, params any) error {
	_, err := call[api.Empty](s, method, params)
	return err
}

func (s *Service) Overview() api.Overview {
	overview, err := call[api.Overview](s, "config.overview", api.Empty{})
	s.mu.Lock()
	if err == nil {
		s.overview = overview
	} else {
		overview = s.overview
	}
	if err != nil {
		overview.ConfigError = err.Error()
	}
	overview.Shortcut, overview.ShortcutError = s.shortcut, s.shortcutErr
	s.mu.Unlock()
	return overview
}

func (s *Service) RevealConfig() error {
	path, err := call[string](s, "config.path", api.Empty{})
	if err != nil {
		return err
	}
	return s.host.app.Env.OpenFileManager(path, true)
}

func (s *Service) Shutdown() {
	s.shutdown.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownLimit)
		defer cancel()
		s.mu.Lock()
		s.stopping = true
		if s.logWatchCancel != nil {
			s.logWatchCancel()
		}
		s.mu.Unlock()
		s.cancel()
		s.mu.Lock()
		installed := s.client
		s.mu.Unlock()
		c := installed
		if c != nil {
			select {
			case <-c.Done():
				c = nil
			default:
			}
		}
		if c == nil && s.shutdownConnect != nil {
			var err error
			c, err = s.shutdownConnect(ctx)
			if err != nil && ctx.Err() == nil {
				log.Printf("connect for daemon shutdown: %v", err)
			}
		}
		usedInstalled := c != nil && c == installed
		if c != nil {
			var result api.Empty
			err := c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, &result)
			_ = c.Close()
			if err != nil && usedInstalled && client.TransportError(err) && ctx.Err() == nil && s.shutdownConnect != nil {
				c, err = s.shutdownConnect(ctx)
				if err == nil {
					err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, &result)
					_ = c.Close()
				}
			}
			if err != nil && ctx.Err() == nil {
				log.Printf("daemon shutdown: %v", err)
			}
		}
		if installed != nil && installed != c {
			_ = installed.Close()
		}
		waited := make(chan struct{})
		go func() { s.wg.Wait(); close(waited) }()
		select {
		case <-waited:
		case <-ctx.Done():
		}
	})
}

func (s *Service) Start(id string) error { return action(s, "tunnel.start", api.IDParams{ID: id}) }

func (s *Service) Stop(id string) {
	if err := action(s, "tunnel.stop", api.IDParams{ID: id}); err != nil {
		log.Printf("stop tunnel %s: %v", id, err)
	}
}

func (s *Service) Toggle(id string) error {
	view, err := call[api.TunnelView](s, "tunnel.get", api.IDParams{ID: id})
	if err != nil {
		return err
	}
	switch view.Status.State {
	case "starting", "running", "reconnecting":
		return action(s, "tunnel.stop", api.IDParams{ID: id})
	default:
		return action(s, "tunnel.start", api.IDParams{ID: id})
	}
}

func (s *Service) Logs(id string) []string {
	logs, err := call[[]string](s, "logs.read", api.IDParams{ID: id})
	if err != nil {
		log.Printf("read logs for %s: %v", id, err)
		return []string{}
	}
	if logs == nil {
		return []string{}
	}
	return logs
}

func (s *Service) Findings(id string) []checks.Finding {
	findings, err := call[[]checks.Finding](s, "tunnel.findings", api.IDParams{ID: id})
	if err != nil {
		log.Printf("read findings for %s: %v", id, err)
		return []checks.Finding{}
	}
	if findings == nil {
		return []checks.Finding{}
	}
	return findings
}

func (s *Service) Reload() {
	if err := action(s, "config.reload", api.Empty{}); err != nil {
		log.Printf("reload config: %v", err)
	}
}

func (s *Service) SetSecret(name, value string, kind secret.Kind) error {
	return action(s, "secret.set", api.SetSecretParams{Name: name, Value: value, Kind: kind})
}

func (s *Service) StopOrphan(pid int) error {
	return action(s, "orphan.stop", api.OrphanParams{PID: pid})
}

func (s *Service) AnswerPrompt(id, value string, cancelled bool) {
	if err := action(s, "prompt.answer", api.AnswerPromptParams{ID: id, Value: value, Cancelled: cancelled}); err != nil {
		log.Printf("answer prompt: %v", err)
	}
}

func (s *Service) EvaluateDraft(t model.Tunnel) api.DraftResult {
	result, err := call[api.DraftResult](s, "tunnel.evaluateDraft", api.TunnelParams{Tunnel: t})
	if err != nil {
		log.Printf("evaluate draft: %v", err)
	}
	return result
}

func (s *Service) NewDraft(kind model.TunnelType) model.Tunnel {
	draft, err := call[model.Tunnel](s, "tunnel.newDraft", api.NewDraftParams{Kind: kind})
	if err != nil {
		log.Printf("new draft: %v", err)
	}
	return draft
}

func (s *Service) ImportCommand(line string) (importer.Result, error) {
	return call[importer.Result](s, "tunnel.import", api.ImportParams{Line: line})
}

func (s *Service) DuplicateDraft(id string) (model.Tunnel, error) {
	return call[model.Tunnel](s, "tunnel.duplicateDraft", api.IDParams{ID: id})
}

func (s *Service) SaveTunnel(t model.Tunnel, start bool) (string, error) {
	return call[string](s, "tunnel.save", api.SaveTunnelParams{Tunnel: t, Start: start})
}

func (s *Service) DeleteTunnel(id string) error {
	return action(s, "tunnel.delete", api.IDParams{ID: id})
}

func (s *Service) SetFavorite(id string, favorite bool) error {
	return action(s, "tunnel.setFavorite", api.FavoriteParams{ID: id, Favorite: favorite})
}

func (s *Service) Restart(id string) error { return action(s, "tunnel.restart", api.IDParams{ID: id}) }

func (s *Service) ActivateProfile(id string) error {
	return action(s, "profile.activate", api.IDParams{ID: id})
}

func (s *Service) SaveProfile(profile model.Profile, members []string) (string, error) {
	return call[string](s, "profile.save", api.SaveProfileParams{Profile: profile, Members: members})
}

func (s *Service) DeleteProfile(id string) error {
	return action(s, "profile.delete", api.IDParams{ID: id})
}

func (s *Service) DuplicateProfile(id string) (string, error) {
	return call[string](s, "profile.duplicate", api.IDParams{ID: id})
}

func (s *Service) SetProfileAutostart(id, tunnelID string, on bool) error {
	return action(s, "profile.setAutostart", api.AutostartParams{ID: id, TunnelID: tunnelID, On: on})
}

func (s *Service) StartProfile(id string) error {
	return action(s, "profile.start", api.IDParams{ID: id})
}

func (s *Service) StopProfile(id string) {
	if err := action(s, "profile.stop", api.IDParams{ID: id}); err != nil {
		log.Printf("stop profile %s: %v", id, err)
	}
}

func (s *Service) SSHHosts(profileID string) api.SSHHostList {
	hosts, err := call[api.SSHHostList](s, "ssh.hosts", api.SSHParams{ProfileID: profileID})
	if err != nil {
		log.Printf("read SSH hosts: %v", err)
	}
	return hosts
}

func (s *Service) ResolveSSHHost(alias, profileID string) (sshconfig.Resolved, error) {
	return call[sshconfig.Resolved](s, "ssh.resolve", api.SSHParams{Alias: alias, ProfileID: profileID})
}

func (s *Service) ReloadSSHHosts() {
	if err := action(s, "ssh.reload", api.Empty{}); err != nil {
		log.Printf("reload SSH hosts: %v", err)
	}
}

func (s *Service) Tools() []api.ToolInfo {
	tools, err := call[[]api.ToolInfo](s, "tool.list", api.Empty{})
	if err != nil {
		log.Printf("read tools: %v", err)
		return []api.ToolInfo{}
	}
	if tools == nil {
		return []api.ToolInfo{}
	}
	return tools
}

func (s *Service) SetToolPath(tool, path string) ([]api.ToolInfo, error) {
	return call[[]api.ToolInfo](s, "tool.setPath", api.ToolPathParams{Tool: tool, Path: path})
}

func (s *Service) ListSecrets() []api.SecretView {
	secrets, err := call[[]api.SecretView](s, "secret.list", api.Empty{})
	if err != nil {
		log.Printf("list secrets: %v", err)
		return []api.SecretView{}
	}
	if secrets == nil {
		return []api.SecretView{}
	}
	return secrets
}

func (s *Service) DeleteSecret(name string) error {
	return action(s, "secret.delete", api.SecretNameParams{Name: name})
}

func (s *Service) RenameSecret(from, to string) error {
	return action(s, "secret.rename", api.RenameSecretParams{From: from, To: to})
}

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
	if id == "" {
		return []supervisor.LogLine{}
	}
	result, generation, err := s.logsSince(ctx, id, 0)
	if err != nil {
		log.Printf("watch logs for %s: %v", id, err)
	}
	seq := result.Sequence
	usedGeneration := generation
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				currentGeneration := s.clientGeneration
				s.mu.Unlock()
				after := seq
				generationReset := usedGeneration != 0 && currentGeneration != usedGeneration
				if usedGeneration == 0 || generationReset {
					after = 0
				}
				next, nextGeneration, err := s.logsSince(ctx, id, after)
				if err != nil {
					continue
				}
				generationReset = usedGeneration != 0 && nextGeneration != usedGeneration
				reset := generationReset || next.Sequence < seq
				if reset {
					if after != 0 {
						next, nextGeneration, err = s.logsSince(ctx, id, 0)
						if err != nil {
							continue
						}
					}
				}
				if ctx.Err() != nil {
					return
				}
				if !reset && len(next.Lines) > 0 {
					lines := next.Lines[:0]
					for _, line := range next.Lines {
						if line.Seq > seq {
							lines = append(lines, line)
						}
					}
					next.Lines = lines
				}
				seq = next.Sequence
				usedGeneration = nextGeneration
				if len(next.Lines) > 0 {
					s.emitLogAppend(api.LogAppend{ID: id, Lines: next.Lines})
				}
			}
		}
	}()
	if err != nil {
		return []supervisor.LogLine{}
	}
	return result.Lines
}

var errConnectionChanged = errors.New("daemon connection changed during call")

func (s *Service) logsSince(ctx context.Context, id string, sequence uint64) (api.LogsResult, uint64, error) {
	s.mu.Lock()
	c := s.client
	generation := s.clientGeneration
	err := s.connectionErr
	s.mu.Unlock()
	if c == nil {
		if err == nil {
			err = errors.New("connection is not ready")
		}
		return api.LogsResult{}, generation, fmt.Errorf("Cannot reach the tunnel service: %w", err)
	}
	var result api.LogsResult
	if err = c.Call(ctx, "logs.since", api.LogsParams{ID: id, Sequence: sequence}, &result); err != nil {
		select {
		case <-c.Done():
			s.markDisconnected(c, err)
		default:
		}
		return api.LogsResult{}, generation, err
	}
	s.mu.Lock()
	changed := s.client != c || s.clientGeneration != generation
	s.mu.Unlock()
	if changed {
		return api.LogsResult{}, generation, errConnectionChanged
	}
	return result, generation, nil
}

func (s *Service) emitLogAppend(append api.LogAppend) {
	s.mu.Lock()
	desktop := s.host
	sink := s.logSink
	s.mu.Unlock()
	if sink != nil {
		sink(append)
	}
	if desktop != nil {
		desktop.app.Event.Emit("logs:appended", append)
	}
}

func (s *Service) emitSettingsChanged() {
	s.mu.Lock()
	desktop := s.host
	sink := s.settingsSink
	s.mu.Unlock()
	if sink != nil {
		sink()
	}
	if desktop != nil {
		desktop.app.Event.Emit(eventSettings)
	}
}

func (s *Service) OpenIn(screen, param string) {
	if s.host != nil {
		s.host.app.Event.Emit("nav:open", map[string]string{"screen": screen, "param": param})
		s.ShowMain()
	}
}

func (s *Service) StartAndCopy(id string) error {
	if err := action(s, "tunnel.start", api.IDParams{ID: id}); err != nil {
		return err
	}
	if err := action(s, "tunnel.recordUsed", api.IDParams{ID: id}); err != nil {
		log.Printf("record tunnel use for %s: %v", id, err)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		status, err := callContext[supervisor.Status](ctx, s, "tunnel.waitReady", api.IDParams{ID: id})
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

func (s *Service) wake() {
	if err := action(s, "system.wake", api.Empty{}); err != nil {
		log.Printf("system wake: %v", err)
	}
}

func (s *Service) focus() {
	if err := action(s, "system.focus", api.Empty{}); err != nil {
		log.Printf("system focus: %v", err)
	}
}

func (s *Service) startMarked() {
	s.mu.Lock()
	if s.startRequested {
		s.mu.Unlock()
		return
	}
	s.startRequested = true
	s.startPending = true
	s.mu.Unlock()
	s.flushStartMarked()
}

func (s *Service) shortcutSetting() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.Shortcut
}

func (s *Service) applicationReady() {
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
}

func (s *Service) flushStartMarked() {
	s.mu.Lock()
	if !s.startPending || s.startInFlight || s.client == nil {
		s.mu.Unlock()
		return
	}
	s.startInFlight = true
	s.mu.Unlock()
	err := action(s, "tunnel.startOnLaunch", api.Empty{})
	s.mu.Lock()
	s.startInFlight = false
	if err == nil {
		s.startPending = false
	}
	s.mu.Unlock()
	if err != nil {
		log.Printf("start marked tunnels: %v", err)
	}
}
