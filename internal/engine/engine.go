// Package engine owns tunnel state and orchestration without a GUI dependency.
package engine

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

	"github.com/trieu04/devt-tunnel/internal/api"
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

const notifyDelay = 50 * time.Millisecond

type Options struct {
	Version       string
	ConfigPath    string
	SettingsPath  string
	StateDir      string
	Secrets       secret.Store
	Runner        supervisor.Runner
	AskpassHelper string
}

type Engine struct {
	configPath        string
	settingsPath      string
	secrets           *secret.Cached
	secretIndex       secretIndexStore
	configStore       configurationStore
	stateStore        *corestate.Store
	settings          settings.Settings
	saveSettings      func(string, settings.Settings) error
	beforeSecretIndex func()
	tools             *platform.Tools
	records           *supervisor.FileRecords
	manager           *supervisor.Manager
	broker            *askpass.Broker
	shell             []string

	configMu       sync.Mutex
	settingsMu     sync.Mutex
	secretIndexMu  sync.Mutex
	lifecycleMu    sync.RWMutex
	promptMu       sync.Mutex
	mu             sync.Mutex
	configError    string
	orphans        []supervisor.Record
	prompts        map[string]*waitingPrompt
	nextPrompt     int
	notifyTimer    *time.Timer
	subscribers    map[uint64]func(api.Event)
	nextSubscriber uint64
	done           chan struct{}
	shutdown       sync.Once
	closed         bool
}

type waitingPrompt struct {
	view   api.PendingPrompt
	answer chan promptAnswer
}

type configurationStore interface {
	Load() (model.Config, error)
	Changed() (bool, error)
	Update(func(*model.Config) error) (model.Config, error)
}

func (s *Engine) updateConfig(change func(*model.Config) error) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	cfg, err := s.configStore.Update(change)
	if stopped := s.lifetimeError(); stopped != nil {
		return stopped
	}
	if err == nil {
		return s.applyWhileAlive(func() { s.manager.SetConfig(cfg) })
	}
	return err
}

type promptAnswer struct {
	value     string
	cancelled bool
}

func New(opts Options) (*Engine, error) {
	var err error
	if opts.ConfigPath == "" {
		opts.ConfigPath, err = platform.ConfigFile()
		if err != nil {
			return nil, err
		}
	}
	if opts.SettingsPath == "" {
		opts.SettingsPath, err = platform.SettingsFile()
		if err != nil {
			return nil, err
		}
	}
	if opts.StateDir == "" {
		opts.StateDir, err = platform.StateDir()
		if err != nil {
			return nil, err
		}
	}
	appSettings, err := settings.Load(opts.SettingsPath)
	if err != nil {
		return nil, err
	}
	if opts.Secrets == nil {
		opts.Secrets = platform.Keychain{}
	}
	if opts.Runner == nil {
		opts.Runner = platform.Runner{}
	}
	if opts.AskpassHelper == "" {
		opts.AskpassHelper = askpassHelperPath()
	}
	index := &secret.Index{Path: filepath.Join(opts.StateDir, "vault.json")}
	done := make(chan struct{})
	cachedSecrets := &secret.Cached{Inner: &lifetimeSecrets{inner: opts.Secrets, done: done}}
	s := &Engine{
		configPath:   opts.ConfigPath,
		settingsPath: opts.SettingsPath,
		secrets:      cachedSecrets,
		secretIndex:  index,
		configStore:  config.NewStore(opts.ConfigPath),
		stateStore:   corestate.NewStore(filepath.Join(opts.StateDir, "state.json")),
		settings:     appSettings,
		saveSettings: settings.Save,
		tools:        &platform.Tools{},
		records:      &supervisor.FileRecords{Path: filepath.Join(opts.StateDir, "processes.json")},
		shell:        platform.ShellFor(appSettings.Shell),
		prompts:      map[string]*waitingPrompt{},
		subscribers:  map[uint64]func(api.Event){},
		done:         done,
	}
	cachedSecrets.Used = s.markSecretUsed
	s.tools.SetOverrides(appSettings.Tools)
	s.broker, err = askpass.NewBroker(opts.AskpassHelper, s.secrets, s.askUser)
	if err != nil {
		return nil, fmt.Errorf("askpass: %w", err)
	}
	s.manager = supervisor.New(supervisor.Deps{
		Runner: opts.Runner, Secrets: s.secrets, Resolve: s.tools.Resolve, Environ: s.tools.Environ,
		Shell: s.shell, Home: platform.Home(), Dialect: platform.Dialect(), Askpass: s.broker,
		Records: s.records, OnChange: s.notify,
	})
	s.manager.SetActiveProfile(s.stateStore.Get().ActiveProfile)
	s.findOrphans()
	s.loadConfig()
	go s.monitorClockJump()
	return s, nil
}

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

func (s *Engine) loadConfig() error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	cfg, err := s.configStore.Load()
	if stopped := s.lifetimeError(); stopped != nil {
		return stopped
	}
	if err != nil {
		if stopped := s.applyWhileAlive(func() {
			s.mu.Lock()
			s.configError = err.Error()
			s.mu.Unlock()
		}); stopped != nil {
			return stopped
		}
		log.Printf("config: %v", err) // keep the last good definitions
		s.notify()
		return err
	}
	active := s.stateStore.Get().ActiveProfile
	if active != "" {
		found := false
		for _, profile := range cfg.Profiles {
			found = found || profile.ID == active
		}
		if !found {
			if err := s.lifetimeError(); err != nil {
				return err
			}
			_ = s.stateStore.Update(func(st *corestate.State) { st.ActiveProfile = "" })
			active = ""
		}
	}
	if err := s.lifetimeError(); err != nil {
		return err
	}
	return s.applyWhileAlive(func() {
		s.mu.Lock()
		s.configError = ""
		s.mu.Unlock()
		s.manager.SetConfig(cfg)
		s.manager.SetActiveProfile(active)
	})
}

func (s *Engine) findOrphans() {
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

func (s *Engine) Overview() api.Overview {
	s.mu.Lock()
	o := api.Overview{
		ConfigPath:  s.configPath,
		ConfigError: s.configError,
		Orphans:     append([]supervisor.Record{}, s.orphans...),
		Prompts:     []api.PendingPrompt{},
	}
	s.mu.Unlock()
	s.promptMu.Lock()
	for _, p := range s.prompts {
		o.Prompts = append(o.Prompts, p.view)
	}
	s.promptMu.Unlock()
	sort.Slice(o.Prompts, func(i, j int) bool { return o.Prompts[i].ID < o.Prompts[j].ID })
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

func (s *Engine) views() []api.TunnelView {
	statuses := s.manager.Statuses()
	tunnels := s.manager.Config().Tunnels
	views := make([]api.TunnelView, 0, len(tunnels))
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
		views = append(views, api.TunnelView{Tunnel: t, Status: statuses[i], Endpoints: endpoints(t, statuses[i]), Preview: preview, MissingSecrets: missing, ToolPath: toolPath})
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

func (s *Engine) Start(id string) error { return s.manager.Start(id) }

func (s *Engine) Stop(id string) { s.manager.Stop(id) }

func (s *Engine) Toggle(id string) error {
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

func (s *Engine) Logs(id string) []string {
	if logs := s.manager.Logs(id); logs != nil {
		return logs
	}
	return []string{}
}

func (s *Engine) Findings(id string) []checks.Finding {
	cfg := s.manager.Config()
	for _, t := range cfg.Tunnels {
		if t.ID == id {
			found := checks.Run(t, cfg, s.checkDeps(t))
			if found == nil {
				return []checks.Finding{}
			}
			return found
		}
	}
	return []checks.Finding{}
}

func (s *Engine) Reload() error { return s.loadConfig() }

func (s *Engine) SetSecret(name, value string, kind secret.Kind) error {
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
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("secret storage may have completed, but shutdown canceled confirmation: %w", err)
		}
		return err
	}
	if err := s.withSecretIndex(func(index secretIndexStore) error { return index.Upsert(name, kind) }); err != nil {
		return fmt.Errorf("secret was stored, but its metadata was not updated: %w", err)
	}
	if err := s.lifetimeError(); err != nil {
		return fmt.Errorf("secret was stored, but shutdown canceled its refresh: %w", err)
	}
	s.manager.SecretsChanged()
	if err := s.applyWhileAlive(s.notify); err != nil {
		return fmt.Errorf("secret was stored, but shutdown canceled its refresh: %w", err)
	}
	return nil
}

func (s *Engine) StopOrphan(pid int) error {
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

func (s *Engine) AnswerPrompt(id, value string, cancelled bool) {
	s.promptMu.Lock()
	p, ok := s.prompts[id]
	delete(s.prompts, id)
	s.promptMu.Unlock()
	if ok {
		p.answer <- promptAnswer{value: value, cancelled: cancelled}
	}
	s.notify()
}

func (s *Engine) askUser(ctx context.Context, prompt askpass.Prompt) (string, error) {
	s.promptMu.Lock()
	s.nextPrompt++
	id := fmt.Sprintf("prompt-%d", s.nextPrompt)
	p := &waitingPrompt{view: api.PendingPrompt{ID: id, Prompt: prompt}, answer: make(chan promptAnswer, 1)}
	s.prompts[id] = p
	s.promptMu.Unlock()
	defer func() {
		s.promptMu.Lock()
		delete(s.prompts, id)
		s.promptMu.Unlock()
		s.notify()
	}()
	s.emit(api.Event{Type: api.EventPromptAdded, Prompt: &p.view})
	s.notify()
	select {
	case <-ctx.Done():
		return "", errors.New("no answer")
	case <-s.done:
		return "", errors.New("engine shut down")
	case a := <-p.answer:
		if a.cancelled {
			return "", errors.New("cancelled by user")
		}
		return a.value, nil
	}
}

func (s *Engine) EnsureConfigFile() (string, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return "", err
	}
	if _, err := os.Stat(s.configPath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
			return "", err
		}
		if err := s.lifetimeError(); err != nil {
			return "", err
		}
		if err := os.WriteFile(s.configPath, []byte(configTemplate), 0o644); err != nil {
			return "", err
		}
	}
	return s.configPath, s.lifetimeError()
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
