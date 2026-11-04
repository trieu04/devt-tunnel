// Package host connects the core to Wails. It is the only package that imports Wails,
// apart from the entry point, and it holds no tunnel logic of its own.
package host

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const (
	eventTunnels = "tunnels:changed"
	eventPrompt  = "askpass:prompt"
	notifyDelay  = 50 * time.Millisecond
)

var Version = "0.1.0"

func (s *Service) AppVersion() string { return Version }

// TunnelView is a tunnel as the UI sees it.
type TunnelView struct {
	Tunnel         model.Tunnel      `json:"tunnel"`
	Status         supervisor.Status `json:"status"`
	Preview        string            `json:"preview"`
	MissingSecrets []string          `json:"missingSecrets"`
	ToolPath       string            `json:"toolPath,omitempty"`
}

// PendingPrompt is an ssh question waiting for the user.
type PendingPrompt struct {
	ID     string         `json:"id"`
	Prompt askpass.Prompt `json:"prompt"`
}

// Overview is everything the UI needs to render its first frame.
type Overview struct {
	ConfigPath        string               `json:"configPath"`
	ConfigError       string               `json:"configError"`
	Shortcut          string               `json:"shortcut"`
	ShortcutError     string               `json:"shortcutError"`
	Profiles          []model.Profile      `json:"profiles"`
	Tunnels           []TunnelView         `json:"tunnels"`
	Orphans           []supervisor.Record  `json:"orphans"`
	Prompts           []PendingPrompt      `json:"prompts"`
	ActiveProfile     string               `json:"activeProfile"`
	LastUsed          map[string]time.Time `json:"lastUsed"`
	KeychainAvailable bool                 `json:"keychainAvailable"`
	KeychainName      string               `json:"keychainName"`
}

// Service is the API the frontend calls through generated bindings.
type Service struct {
	configPath   string
	settingsPath string
	secrets      *secret.Cached
	secretIndex  *secret.Index
	configStore  *config.Store
	stateStore   *corestate.Store
	settings     settings.Settings
	auth         platform.UserAuth
	tools        *platform.Tools
	records      *supervisor.FileRecords
	manager      *supervisor.Manager
	broker       *askpass.Broker
	shell        []string

	host *desktop // set by Attach

	mu             sync.Mutex
	configError    string
	shortcutErr    string
	shortcut       string
	orphans        []supervisor.Record
	prompts        map[string]*waitingPrompt
	nextPrompt     int
	notifyTimer    *time.Timer
	logWatchCancel context.CancelFunc
	updateChecked  time.Time
	updateInfo     UpdateInfo
}

type waitingPrompt struct {
	view   PendingPrompt
	answer chan promptAnswer
}

type promptAnswer struct {
	value     string
	cancelled bool
}

// NewService prepares the core. It starts no tunnels.
func NewService() (*Service, error) {
	configPath, err := platform.ConfigFile()
	if err != nil {
		return nil, err
	}
	stateDir, err := platform.StateDir()
	if err != nil {
		return nil, err
	}
	settingsPath, err := platform.SettingsFile()
	if err != nil {
		return nil, err
	}
	appSettings, err := settings.Load(settingsPath)
	if err != nil {
		return nil, err
	}
	index := &secret.Index{Path: filepath.Join(stateDir, "vault.json")}
	cachedSecrets := &secret.Cached{Inner: platform.Keychain{}, Used: index.MarkUsed}
	s := &Service{
		configPath:   configPath,
		settingsPath: settingsPath,
		secrets:      cachedSecrets,
		secretIndex:  index,
		configStore:  config.NewStore(configPath),
		stateStore:   corestate.NewStore(filepath.Join(stateDir, "state.json")),
		settings:     appSettings,
		auth:         platform.NewUserAuth(),
		tools:        &platform.Tools{},
		records:      &supervisor.FileRecords{Path: filepath.Join(stateDir, "processes.json")},
		shell:        platform.ShellFor(appSettings.Shell),
		prompts:      map[string]*waitingPrompt{},
	}
	s.tools.SetOverrides(appSettings.Tools)
	s.broker, err = askpass.NewBroker(askpassHelperPath(), s.secrets, s.askUser)
	if err != nil {
		return nil, fmt.Errorf("askpass: %w", err)
	}
	s.manager = supervisor.New(supervisor.Deps{
		Runner:   platform.Runner{},
		Secrets:  s.secrets,
		Resolve:  s.tools.Resolve,
		Environ:  s.tools.Environ,
		Shell:    s.shell,
		Home:     platform.Home(),
		Dialect:  platform.Dialect(),
		Askpass:  s.broker,
		Records:  s.records,
		OnChange: s.notify,
	})
	active := s.stateStore.Get().ActiveProfile
	s.manager.SetActiveProfile(active)
	s.findOrphans()
	s.loadConfig()
	return s, nil
}

// askpassHelperPath finds the helper next to the app binary.
func askpassHelperPath() string {
	name := "devt-tunnel-askpass"
	if filepath.Separator == '\\' {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func (s *Service) loadConfig() {
	cfg, err := s.configStore.Load()
	s.mu.Lock()
	if err != nil {
		s.configError = err.Error()
	} else {
		s.configError = ""
	}
	s.mu.Unlock()
	if err != nil {
		log.Printf("config: %v", err) // keep the last good definitions
		s.notify()
		return
	}
	s.manager.SetConfig(cfg)
	active := s.stateStore.Get().ActiveProfile
	if active != "" {
		found := false
		for _, profile := range cfg.Profiles {
			found = found || profile.ID == active
		}
		if !found {
			_ = s.stateStore.Update(func(st *corestate.State) { st.ActiveProfile = "" })
			active = ""
		}
	}
	s.manager.SetActiveProfile(active)
}

// findOrphans keeps records of processes from a crashed run and drops stale ones.
func (s *Service) findOrphans() {
	recs, err := s.records.List()
	if err != nil {
		log.Printf("process records: %v", err)
		return
	}
	orphans := supervisor.FindOrphans(recs, platform.ProcessIdentity)
	alive := map[int]bool{}
	for _, o := range orphans {
		alive[o.PID] = true
	}
	for _, r := range recs {
		if !alive[r.PID] {
			s.records.Remove(r.PID)
		}
	}
	s.mu.Lock()
	s.orphans = orphans
	s.mu.Unlock()
}

// Overview returns the current state of the app.
func (s *Service) Overview() Overview {
	s.mu.Lock()
	o := Overview{
		ConfigPath:    s.configPath,
		ConfigError:   s.configError,
		Shortcut:      s.shortcut,
		ShortcutError: s.shortcutErr,
		Orphans:       append([]supervisor.Record{}, s.orphans...),
		Prompts:       []PendingPrompt{},
	}
	for _, p := range s.prompts {
		o.Prompts = append(o.Prompts, p.view)
	}
	sort.Slice(o.Prompts, func(i, j int) bool { return o.Prompts[i].ID < o.Prompts[j].ID })
	s.mu.Unlock()
	state := s.stateStore.Get()
	o.ActiveProfile = state.ActiveProfile
	o.LastUsed = state.LastUsed
	if o.LastUsed == nil {
		o.LastUsed = map[string]time.Time{}
	}
	o.KeychainName = keychainName()
	_, keychainErr := secret.Has(s.secrets, "__devt_tunnel_availability_probe__")
	o.KeychainAvailable = keychainErr == nil || errors.Is(keychainErr, secret.ErrNotFound)
	o.Profiles = s.manager.Config().Profiles
	o.Tunnels = s.views()
	return o
}

func (s *Service) views() []TunnelView {
	statuses := s.manager.Statuses()
	tunnels := s.manager.Config().Tunnels
	views := make([]TunnelView, 0, len(tunnels))
	for i, t := range tunnels {
		if i >= len(statuses) {
			break // config replaced between the two reads; the next notification corrects it
		}
		preview, err := s.manager.Preview(t.ID)
		if err != nil {
			preview = err.Error()
		}
		missing := []string{}
		for _, name := range t.Secrets() {
			if _, err := secret.Has(s.secrets, name); err != nil {
				missing = append(missing, name)
			}
		}
		spec, _ := command.Build(t, command.Options{Shell: s.shell, Home: platform.Home(), Dialect: platform.Dialect()})
		toolPath, _ := s.tools.Resolve(spec.Tool)
		views = append(views, TunnelView{Tunnel: t, Status: statuses[i], Preview: preview, MissingSecrets: missing, ToolPath: toolPath})
	}
	return views
}

func keychainName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "Secret Service"
	}
}

// Start launches a tunnel.
func (s *Service) Start(id string) error { return s.manager.Start(id) }

// Stop ends a tunnel and waits for it to exit.
func (s *Service) Stop(id string) { s.manager.Stop(id) }

// Toggle starts a stopped tunnel or stops an active one.
func (s *Service) Toggle(id string) error {
	for _, st := range s.manager.Statuses() {
		if st.ID != id {
			continue
		}
		switch st.State {
		case supervisor.StateStarting, supervisor.StateRunning, supervisor.StateReconnecting:
			s.manager.Stop(id)
			return nil
		}
		return s.manager.Start(id)
	}
	return fmt.Errorf("unknown tunnel %q", id)
}

// Logs returns a tunnel's retained output.
func (s *Service) Logs(id string) []string {
	if logs := s.manager.Logs(id); logs != nil {
		return logs
	}
	return []string{}
}

// Findings returns the editor checks for a tunnel.
func (s *Service) Findings(id string) []checks.Finding {
	cfg := s.manager.Config()
	for _, t := range cfg.Tunnels {
		if t.ID == id {
			found := checks.Run(t, cfg, checks.Deps{Resolve: s.tools.Resolve, Secrets: s.secrets, Shell: s.shell})
			if found == nil {
				return []checks.Finding{}
			}
			return found
		}
	}
	return []checks.Finding{}
}

// Reload rereads tunnels.toml.
func (s *Service) Reload() { s.loadConfig() }

// SetSecret stores a value in the OS keychain.
func (s *Service) SetSecret(name, value string, kind secret.Kind) error {
	if !secretName.MatchString(name) {
		return errors.New("secret name may contain only letters, numbers, dot, underscore and hyphen")
	}
	if value == "" {
		return errors.New("secret value is required")
	}
	if len(value) > 2560 {
		return errors.New("secret is too large for the OS credential store")
	}
	if err := s.secrets.Set(name, value); err != nil {
		return err
	}
	_ = s.secretIndex.Upsert(name, kind)
	s.manager.SecretsChanged()
	s.notify()
	return nil
}

// RevealConfig shows tunnels.toml in the file manager, creating it from a template if needed.
func (s *Service) RevealConfig() error {
	if _, err := os.Stat(s.configPath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.configPath, []byte(configTemplate), 0o644); err != nil {
			return err
		}
	}
	return s.host.app.Env.OpenFileManager(s.configPath, true)
}

// StopOrphan ends a process left by a crashed run.
func (s *Service) StopOrphan(pid int) error {
	s.mu.Lock()
	var found bool
	var identity string
	var kept []supervisor.Record
	for _, o := range s.orphans {
		if o.PID == pid {
			found = true
			identity = o.Identity
		} else {
			kept = append(kept, o)
		}
	}
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("process %d is not a known orphan", pid)
	}
	s.mu.Unlock()
	if err := platform.StopOrphan(pid, identity, 5*time.Second); err != nil {
		return err
	}
	s.mu.Lock()
	s.orphans = kept
	s.mu.Unlock()
	s.records.Remove(pid)
	s.notify()
	return nil
}

// AnswerPrompt delivers the user's reply to a waiting ssh prompt.
func (s *Service) AnswerPrompt(id, value string, cancelled bool) {
	s.mu.Lock()
	p, ok := s.prompts[id]
	delete(s.prompts, id)
	s.mu.Unlock()
	if ok {
		p.answer <- promptAnswer{value: value, cancelled: cancelled}
	}
	s.notify()
}

// askUser implements askpass.Asker.
func (s *Service) askUser(ctx context.Context, prompt askpass.Prompt) (string, error) {
	s.mu.Lock()
	s.nextPrompt++
	id := fmt.Sprintf("prompt-%d", s.nextPrompt)
	p := &waitingPrompt{view: PendingPrompt{ID: id, Prompt: prompt}, answer: make(chan promptAnswer, 1)}
	s.prompts[id] = p
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.prompts, id)
		s.mu.Unlock()
		s.notify()
	}()
	if s.host != nil {
		s.host.app.Event.Emit(eventPrompt, p.view)
		s.ShowMain()
	}
	s.notify()
	select {
	case <-ctx.Done():
		return "", errors.New("no answer")
	case a := <-p.answer:
		if a.cancelled {
			return "", errors.New("cancelled by user")
		}
		return a.value, nil
	}
}

// ShowMain brings the main window to the front.
func (s *Service) ShowMain() {
	if s.host != nil {
		s.host.showMain()
	}
}

// HideQuickLaunch closes the quick launch window.
func (s *Service) HideQuickLaunch() {
	if s.host != nil {
		s.host.quick.Hide()
	}
}

// Quit stops all tunnels and exits.
func (s *Service) Quit() {
	if s.host != nil {
		s.host.app.Quit()
	}
}

// Shutdown is called by Wails when the app is exiting.
func (s *Service) Shutdown() {
	s.manager.Shutdown()
	s.broker.Close()
}

// notify coalesces bursts of changes into one UI and tray refresh.
func (s *Service) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyTimer != nil || s.host == nil {
		return
	}
	s.notifyTimer = time.AfterFunc(notifyDelay, func() {
		s.mu.Lock()
		s.notifyTimer = nil
		s.mu.Unlock()
		s.host.app.Event.Emit(eventTunnels)
		s.host.refreshTray(s.views())
	})
}

const configTemplate = `# devt-tunnel tunnel definitions. See docs/architecture.md for all fields.
# After editing, press Reload in the app.

# [[profile]]
# id = "work"
# name = "Work"

# [[tunnel]]
# id = "db"
# name = "Production DB"
# type = "ssh"
# profiles = ["work"]
# favorite = true
# reconnect = true
#
# [tunnel.ssh]
# host = "server1"                  # alias from ~/.ssh/config
# flags = ["-N"]
# options = { ServerAliveInterval = "30" }
#
# [[tunnel.forward]]
# kind = "L"
# bind = "127.0.0.1:5433"
# target = "127.0.0.1:5432"
`
