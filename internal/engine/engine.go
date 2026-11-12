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
	"sync/atomic"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
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
	notifyDelay             = 50 * time.Millisecond
	overviewKeychainTimeout = 250 * time.Millisecond
)

type keychainProbe struct {
	done chan struct{}
	err  error
}

type Options struct {
	Audit           *audit.Writer // Owned and closed by the daemon under its lifecycle lock.
	Version         string
	ConfigPath      string
	SettingsPath    string
	StateDir        string
	Secrets         secret.Store
	Runner          supervisor.Runner
	AskpassHelper   string
	Clock           Clock
	PortRange       string
	Listening       func(string) bool
	ProcessIdentity func(int) (string, error)
	StopProcess     func(int, string, time.Duration) error
}

type Engine struct {
	// Request serialization is outside configMu: execution reuses the normal
	// lease/ephemeral admission methods, which acquire configMu themselves.
	requestMu           sync.Mutex
	openRequests        map[openRequestKey]*openRequest
	denialMu            sync.Mutex
	denials             map[string]*denialWindow
	redactionMu         sync.Mutex
	redactions          map[string]*tunnelRedaction
	audit               *audit.Writer
	auditReader         *audit.Reader
	approvals           map[string]*approvalRequest
	approvalOrder       []string
	runtimeMu           sync.RWMutex
	runtime             corestate.Runtime
	runtimeStore        corestate.RuntimeStore
	clock               Clock
	portFirst, portLast int
	portHistory         map[string]map[int]bool
	listening           func(string) bool
	processIdentity     func(int) (string, error)
	stopProcess         func(int, string, time.Duration) error
	runtimeChanged      chan struct{}
	sessions            map[string]sessionState

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

	configMu        sync.Mutex
	settingsMu      sync.Mutex
	secretIndexMu   sync.Mutex
	keychainProbeMu sync.Mutex
	keychainProbe   *keychainProbe
	lifecycleMu     sync.RWMutex
	promptMu        sync.Mutex
	orphanMu        sync.Mutex
	mu              sync.Mutex
	configError     string
	orphans         []supervisor.Record
	orphanCount     atomic.Int64
	prompts         map[string]*waitingPrompt
	nextPrompt      int
	notifyTimer     *time.Timer
	subscribers     map[uint64]func(api.Event)
	nextSubscriber  uint64
	done            chan struct{}
	shutdown        sync.Once
	closed          bool
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
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		if err := change(cfg); err != nil {
			return err
		}
		return invalidSpec(s.merged(*cfg).Validate())
	})
	if stopped := s.lifetimeError(); stopped != nil {
		return stopped
	}
	if err == nil {
		return s.applyWhileAlive(func() { s.setManagerConfig(s.merged(cfg)) })
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
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.PortRange == "" {
		opts.PortRange = appSettings.AgentPolicy().PortRange
	}
	first, last, err := parsePortRange(opts.PortRange)
	if err != nil {
		return nil, err
	}
	if opts.Listening == nil {
		opts.Listening = listeningLoopback
	}
	if opts.ProcessIdentity == nil {
		opts.ProcessIdentity = platform.ProcessIdentity
	}
	if opts.StopProcess == nil {
		opts.StopProcess = platform.StopOrphan
	}
	runtimeStore := corestate.RuntimeStore{Path: filepath.Join(opts.StateDir, "runtime.json")}
	runtimeState, err := runtimeStore.Load()
	if err != nil {
		return nil, fmt.Errorf("runtime: %w", err)
	}
	index := &secret.Index{Path: filepath.Join(opts.StateDir, "vault.json")}
	done := make(chan struct{})
	cachedSecrets := &secret.Cached{Inner: &lifetimeSecrets{inner: opts.Secrets, done: done}}
	s := &Engine{
		audit: opts.Audit, auditReader: audit.NewReader(opts.StateDir), approvals: map[string]*approvalRequest{},
		configPath: opts.ConfigPath,
		runtime:    runtimeState, runtimeStore: runtimeStore, clock: opts.Clock,
		portFirst: first, portLast: last, listening: opts.Listening, portHistory: map[string]map[int]bool{},
		processIdentity: opts.ProcessIdentity, stopProcess: opts.StopProcess,
		runtimeChanged: make(chan struct{}, 1), sessions: map[string]sessionState{},
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
	cachedSecrets.Inner.(*lifetimeSecrets).observe = s.observeRedactionSecret
	s.tools.SetOverrides(appSettings.Tools)
	s.broker, err = askpass.NewBroker(opts.AskpassHelper, s.secrets, s.askUser)
	if err != nil {
		return nil, fmt.Errorf("askpass: %w", err)
	}
	s.manager = supervisor.New(supervisor.Deps{
		Runner: opts.Runner, Secrets: s.secrets, Resolve: s.tools.Resolve, Environ: s.tools.Environ,
		Shell: s.shell, Home: platform.Home(), Dialect: platform.Dialect(), Askpass: s.broker,
		Records: s.records, OnChange: s.managerChanged, RetryForward: s.retryForward,
	})
	s.findOrphans()
	s.recoverPromotions()
	s.loadConfig()
	if err := s.recoverRuntime(); err != nil {
		s.Shutdown()
		return nil, err
	}
	go s.monitorRuntime()
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
	if err == nil {
		err = invalidSpec(s.merged(cfg).Validate())
	}
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
	if err := s.lifetimeError(); err != nil {
		return err
	}
	return s.applyWhileAlive(func() {
		s.mu.Lock()
		s.configError = ""
		s.mu.Unlock()
		s.setManagerConfig(s.merged(cfg))
	})
}

func (s *Engine) findOrphans() {
	recs, err := s.records.List()
	if err != nil {
		log.Printf("process records: %v", err)
		return
	}
	orphans := supervisor.FindOrphans(recs, s.processIdentity)
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
	s.orphanCount.Store(int64(len(orphans)))
	s.mu.Unlock()
}

// refreshOrphans prunes only recovered children, never adopts manager-owned
// records. A child can exit on its own while a lease is in its grace period.
func (s *Engine) refreshOrphans() {
	s.orphanMu.Lock()
	defer s.orphanMu.Unlock()
	s.mu.Lock()
	recovered := append([]supervisor.Record(nil), s.orphans...)
	s.mu.Unlock()
	alive := make([]supervisor.Record, 0, len(recovered))
	for _, record := range recovered {
		if identity, err := s.processIdentity(record.PID); err == nil && identity == record.Identity {
			alive = append(alive, record)
		} else {
			_ = s.records.Remove(record.PID)
		}
	}
	if len(alive) != len(recovered) {
		s.mu.Lock()
		s.orphans = alive
		s.orphanCount.Store(int64(len(alive)))
		s.mu.Unlock()
		s.notify()
	}
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
	o.LastUsed = state.LastUsed
	if o.LastUsed == nil {
		o.LastUsed = map[string]time.Time{}
	}
	o.KeychainName = keychainName()
	o.KeychainAvailable = s.keychainAvailable(overviewKeychainTimeout)
	o.Profiles = s.manager.Config().Profiles
	o.Groups = s.manager.Config().Groups
	o.Tunnels = s.tunnelViewsFor(api.Actor{Kind: "gui"}, o.KeychainAvailable)
	return o
}

func (s *Engine) keychainAvailable(timeout time.Duration) bool {
	s.keychainProbeMu.Lock()
	probe := s.keychainProbe
	if probe == nil {
		probe = &keychainProbe{done: make(chan struct{})}
		s.keychainProbe = probe
		go func() {
			_, probe.err = secret.Has(s.secrets, "__devt_tunnel_availability_probe__")
			close(probe.done)
			s.keychainProbeMu.Lock()
			if s.keychainProbe == probe {
				s.keychainProbe = nil
			}
			s.keychainProbeMu.Unlock()
		}()
	}
	s.keychainProbeMu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-probe.done:
		return probe.err == nil || errors.Is(probe.err, secret.ErrNotFound)
	case <-timer.C:
		return false
	}
}

func (s *Engine) views() []api.TunnelView { return s.TunnelViewsFor(api.Actor{Kind: "gui"}) }
func (s *Engine) TunnelViewsFor(actor api.Actor) []api.TunnelView {
	return s.tunnelViewsFor(actor, actor.Kind != "mcp")
}

func (s *Engine) tunnelViewsFor(actor api.Actor, keychainAvailable bool) []api.TunnelView {
	statuses := s.manager.RuntimeStatuses()
	if actor.Kind != "mcp" && keychainAvailable {
		statuses = s.manager.Statuses()
	}
	byID := map[string]supervisor.Status{}
	for _, status := range statuses {
		byID[status.ID] = status
	}
	tunnels := s.manager.Config().Tunnels
	views := make([]api.TunnelView, 0, len(tunnels))
	for _, t := range tunnels {
		status, ok := byID[t.ID]
		if !ok {
			continue
		} // config changed between snapshots; the next event resyncs it
		preview, toolPath := "", ""
		missing := []string{}
		if actor.Kind != "mcp" {
			var err error
			preview, err = s.manager.Preview(t.ID)
			if err != nil {
				preview = err.Error()
			}

			if keychainAvailable {
				for _, name := range t.Secrets() {
					if _, err := secret.Has(s.secrets, name); err != nil {
						missing = append(missing, name)
					}
				}
			} else if status.State == supervisor.StateStopped && len(t.Secrets()) > 0 {
				status.State = supervisor.StateKeyringUnavailable
			}
			spec, _ := command.Build(t, command.Options{Shell: s.shell, Home: platform.Home(), Dialect: platform.Dialect()})
			toolPath, _ = s.tools.Resolve(spec.Tool)
		}
		owner := s.owner(t.ID)
		status.Owner = owner
		views = append(views, api.TunnelView{Owner: owner, Tunnel: t, Status: status, Endpoints: endpoints(t, status), Preview: preview, MissingSecrets: missing, ToolPath: toolPath})
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

func (s *Engine) Start(id string) error { return s.personStart(id, false) }

func (s *Engine) Stop(id string) {
	if err := s.StopTunnel(id); err != nil {
		log.Printf("stop %s: %v", id, err)
	}
}

func (s *Engine) Toggle(id string) error {
	for _, st := range s.manager.RuntimeStatuses() {
		if st.ID == id {
			if activeState(st.State) {
				return s.StopTunnel(id)
			}
			return s.Start(id)
		}
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
	return s.FindingsFor(api.Actor{Kind: "gui"}, id)
}
func (s *Engine) FindingsFor(actor api.Actor, id string) []checks.Finding {
	cfg := s.manager.Config()
	for _, t := range cfg.Tunnels {
		if t.ID == id {
			deps := s.checkDeps(t)
			deps.SkipSecretPresence = actor.Kind == "mcp"
			found := checks.Run(t, cfg, deps)
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
	s.orphanMu.Lock()
	defer s.orphanMu.Unlock()
	s.mu.Lock()
	var found bool
	var identity string
	for _, o := range s.orphans {
		if o.PID == pid {
			found = true
			identity = o.Identity
		}
	}
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("process %d is not a known orphan", pid)
	}
	s.mu.Unlock()
	if err := s.stopProcess(pid, identity, 5*time.Second); err != nil {
		return err
	}
	s.mu.Lock()
	kept := s.orphans[:0]
	for _, orphan := range s.orphans {
		if orphan.PID != pid {
			kept = append(kept, orphan)
		}
	}
	s.orphans = kept
	s.orphanCount.Store(int64(len(kept)))
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

# [[group]]
# id = "backend"
# name = "Backend"

# [[tunnel]]
# id = "db"
# name = "Production DB"
# type = "ssh"
# profile = "work"
# groups = ["backend"]
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
