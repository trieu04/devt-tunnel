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
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const (
	eventTunnels = "tunnels:changed"
	eventPrompt  = "askpass:prompt"
	notifyDelay  = 50 * time.Millisecond
)

// TunnelView is a tunnel as the UI sees it.
type TunnelView struct {
	Tunnel  model.Tunnel      `json:"tunnel"`
	Status  supervisor.Status `json:"status"`
	Preview string            `json:"preview"`
}

// PendingPrompt is an ssh question waiting for the user.
type PendingPrompt struct {
	ID     string         `json:"id"`
	Prompt askpass.Prompt `json:"prompt"`
}

// Overview is everything the UI needs to render its first frame.
type Overview struct {
	ConfigPath    string              `json:"configPath"`
	ConfigError   string              `json:"configError"`
	Shortcut      string              `json:"shortcut"`
	ShortcutError string              `json:"shortcutError"`
	Profiles      []model.Profile     `json:"profiles"`
	Tunnels       []TunnelView        `json:"tunnels"`
	Orphans       []supervisor.Record `json:"orphans"`
	Prompts       []PendingPrompt     `json:"prompts"`
}

// Service is the API the frontend calls through generated bindings.
type Service struct {
	configPath string
	secrets    secret.Store
	tools      *platform.Tools
	records    *supervisor.FileRecords
	manager    *supervisor.Manager
	broker     *askpass.Broker
	shell      []string

	host *desktop // set by Attach

	mu          sync.Mutex
	configError string
	shortcutErr string
	shortcut    string
	orphans     []supervisor.Record
	prompts     map[string]*waitingPrompt
	nextPrompt  int
	notifyTimer *time.Timer
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
	s := &Service{
		configPath: configPath,
		secrets:    platform.Keychain{},
		tools:      &platform.Tools{},
		records:    &supervisor.FileRecords{Path: filepath.Join(stateDir, "processes.json")},
		shell:      platform.Shell(),
		prompts:    map[string]*waitingPrompt{},
	}
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
		Askpass:  s.broker,
		Records:  s.records,
		OnChange: s.notify,
	})
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
	return filepath.Join(filepath.Dir(exe), name)
}

func (s *Service) loadConfig() {
	cfg, err := config.Load(s.configPath)
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
	s.mu.Unlock()
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
		views = append(views, TunnelView{Tunnel: t, Status: statuses[i], Preview: preview})
	}
	return views
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
func (s *Service) SetSecret(name, value string) error {
	if name == "" || value == "" {
		return errors.New("secret name and value are required")
	}
	if err := s.secrets.Set(name, value); err != nil {
		return err
	}
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
	var kept []supervisor.Record
	for _, o := range s.orphans {
		if o.PID == pid {
			found = true
		} else {
			kept = append(kept, o)
		}
	}
	if !found {
		s.mu.Unlock()
		return fmt.Errorf("process %d is not a known orphan", pid)
	}
	s.orphans = kept
	s.mu.Unlock()
	err := platform.KillOrphan(pid)
	s.records.Remove(pid)
	s.notify()
	return err
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
