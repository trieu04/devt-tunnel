// Package supervisor runs tunnels as child processes and tracks their state.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type State string

const (
	StateStopped            State = "stopped"
	StateStarting           State = "starting"
	StateRunning            State = "running"
	StateReconnecting       State = "reconnecting"
	StateFailed             State = "failed"
	StateNeedsToken         State = "needs_token"
	StateKeyringUnavailable State = "keyring_unavailable"
)

// SpawnRequest is the OS-level process to start.
type SpawnRequest struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Output io.Writer // receives stdout and stderr
}

// Process is a started child, including anything it spawned.
type Process interface {
	PID() int
	Identity() string
	Wait() error                    // blocks until exit
	Stop(grace time.Duration) error // ends the whole tree
}

// Runner starts processes. internal/platform provides the real one.
type Runner interface {
	Start(SpawnRequest) (Process, error)
}

// Askpass registers a spawn with the prompt broker and returns its environment.
type Askpass interface {
	Register(tunnelID, passphraseSecret string) (env []string, release func())
}

// Deps are the supervisor's connections to the outside world.
type Deps struct {
	Runner   Runner
	Secrets  secret.Store
	Resolve  func(tool string) (string, error) // finds a binary
	Environ  func() []string                   // base environment for children
	Shell    []string                          // prefix for command tunnels
	Askpass  Askpass                           // optional
	Records  Records                           // optional
	OnChange func()                            // optional; called after any state change

	// Zero values select the defaults below.
	BackoffBase  time.Duration
	BackoffMax   time.Duration
	ReadyTimeout time.Duration
	StopGrace    time.Duration
	LogLines     int
	PollInterval time.Duration
}

const (
	defaultBackoffBase = 5 * time.Second
	defaultBackoffMax  = time.Minute
	// Long enough for a person to answer an SSH prompt during startup.
	defaultReadyTimeout = 3 * time.Minute
	defaultStopGrace    = 5 * time.Second
	defaultLogLines     = 2000
	defaultPoll         = 200 * time.Millisecond
)

// Status is a tunnel's current runtime state.
type Status struct {
	ID        string    `json:"id"`
	State     State     `json:"state"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
	Address   string    `json:"address,omitempty"`
	Attempt   int       `json:"attempt,omitempty"`
}

// Manager owns all tunnels.
type Manager struct {
	deps Deps

	mu      sync.Mutex
	config  model.Config
	tunnels map[string]*entry
	closed  bool
}

type entry struct {
	status Status
	log    *logRing
	cancel context.CancelFunc
	done   chan struct{}
}

func New(deps Deps) *Manager {
	if deps.BackoffBase == 0 {
		deps.BackoffBase = defaultBackoffBase
	}
	if deps.BackoffMax == 0 {
		deps.BackoffMax = defaultBackoffMax
	}
	if deps.ReadyTimeout == 0 {
		deps.ReadyTimeout = defaultReadyTimeout
	}
	if deps.StopGrace == 0 {
		deps.StopGrace = defaultStopGrace
	}
	if deps.LogLines == 0 {
		deps.LogLines = defaultLogLines
	}
	if deps.PollInterval == 0 {
		deps.PollInterval = defaultPoll
	}
	return &Manager{deps: deps, tunnels: map[string]*entry{}}
}

// SetConfig replaces the definitions. Tunnels no longer defined are stopped.
// Running tunnels keep their old settings until they are restarted.
func (m *Manager) SetConfig(cfg model.Config) {
	m.mu.Lock()
	m.config = cfg
	defined := map[string]bool{}
	for _, t := range cfg.Tunnels {
		defined[t.ID] = true
		m.entryLocked(t.ID)
	}
	var removed []string
	for id := range m.tunnels {
		if !defined[id] {
			removed = append(removed, id)
		}
	}
	m.mu.Unlock()
	for _, id := range removed {
		m.Stop(id)
		m.mu.Lock()
		delete(m.tunnels, id)
		m.mu.Unlock()
	}
	m.changed()
}

func (m *Manager) Config() model.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config
}

// Statuses returns the state of every defined tunnel, in definition order.
// A tunnel at rest whose secret is missing reports that instead of Stopped.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	tunnels := m.config.Tunnels
	out := make([]Status, 0, len(tunnels))
	for _, t := range tunnels {
		out = append(out, m.entryLocked(t.ID).status)
	}
	m.mu.Unlock()
	// Keychain access can be slow or wait on an OS prompt, so it happens outside the lock.
	for i, t := range tunnels {
		if out[i].State == StateStopped {
			if s := m.secretState(t); s != "" {
				out[i].State = s
			}
		}
	}
	return out
}

// Logs returns the retained output of a tunnel.
func (m *Manager) Logs(id string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.tunnels[id]; ok {
		return e.log.snapshot()
	}
	return nil
}

// Preview returns the command line the tunnel would run.
func (m *Manager) Preview(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.findLocked(id)
	if !ok {
		return "", fmt.Errorf("unknown tunnel %q", id)
	}
	spec, err := command.Build(t, m.optionsLocked(t))
	if err != nil {
		return "", err
	}
	return spec.Preview(), nil
}

// Start launches a tunnel. Starting an active tunnel does nothing.
func (m *Manager) Start(id string) error {
	m.mu.Lock()
	t, ok := m.findLocked(id)
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown tunnel %q", id)
	}
	missing := m.secretState(t)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("shutting down")
	}
	e := m.entryLocked(id)
	if e.cancel != nil {
		return nil
	}
	if missing != "" {
		e.status = Status{ID: id, State: missing}
		go m.changed()
		return nil
	}
	spec, err := command.Build(t, m.optionsLocked(t))
	if err != nil {
		e.status = Status{ID: id, State: StateFailed, Error: err.Error()}
		go m.changed()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	e.status = Status{ID: id, State: StateStarting}
	go m.loop(ctx, t, spec, e)
	go m.changed()
	return nil
}

// Stop ends a tunnel and waits for it to exit.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	e, ok := m.tunnels[id]
	if !ok || e.cancel == nil {
		m.mu.Unlock()
		return
	}
	cancel, done := e.cancel, e.done
	m.mu.Unlock()
	cancel()
	<-done
}

// Shutdown stops every tunnel and refuses further starts.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	ids := make([]string, 0, len(m.tunnels))
	for id := range m.tunnels {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Stop(id)
		}()
	}
	wg.Wait()
}

func (m *Manager) findLocked(id string) (model.Tunnel, bool) {
	for _, t := range m.config.Tunnels {
		if t.ID == id {
			return t, true
		}
	}
	return model.Tunnel{}, false
}

func (m *Manager) entryLocked(id string) *entry {
	e, ok := m.tunnels[id]
	if !ok {
		e = &entry{status: Status{ID: id, State: StateStopped}, log: newLogRing(m.deps.LogLines)}
		m.tunnels[id] = e
	}
	return e
}

// optionsLocked picks the SSH config of the tunnel's first profile that sets one.
func (m *Manager) optionsLocked(t model.Tunnel) command.Options {
	opts := command.Options{Shell: m.deps.Shell}
	for _, pid := range t.Profiles {
		for _, p := range m.config.Profiles {
			if p.ID == pid && p.SSHConfig != "" {
				opts.SSHConfig = p.SSHConfig
				return opts
			}
		}
	}
	return opts
}

func (m *Manager) secretState(t model.Tunnel) State {
	for _, name := range t.Secrets() {
		if _, err := m.deps.Secrets.Get(name); err != nil {
			if errors.Is(err, secret.ErrNotFound) {
				return StateNeedsToken
			}
			return StateKeyringUnavailable
		}
	}
	return ""
}

func (m *Manager) changed() {
	if m.deps.OnChange != nil {
		m.deps.OnChange()
	}
}

func (m *Manager) update(e *entry, f func(*Status)) {
	m.mu.Lock()
	f(&e.status)
	m.mu.Unlock()
	m.changed()
}

// loop runs one tunnel from Start until it is stopped or gives up.
func (m *Manager) loop(ctx context.Context, t model.Tunnel, spec command.Spec, e *entry) {
	defer func() {
		m.mu.Lock()
		e.cancel = nil
		close(e.done)
		m.mu.Unlock()
		m.changed()
	}()
	retrying := false
	backoff := m.deps.BackoffBase
	attempt := 0
	for {
		attempt++
		wasRunning, err := m.runOnce(ctx, t, spec, e, attempt)
		if ctx.Err() != nil {
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: StateStopped} })
			return
		}
		// A first start that fails is reported as Failed. Reconnect only applies once the tunnel has run.
		if !t.Reconnect || (!wasRunning && !retrying) {
			msg := "process exited"
			if err != nil {
				msg = err.Error()
			}
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: StateFailed, Error: msg} })
			return
		}
		if wasRunning {
			backoff = m.deps.BackoffBase
			attempt = 0
		}
		retrying = true
		msg := "process exited"
		if err != nil {
			msg = err.Error()
		}
		e.log.Note(fmt.Sprintf("%s; reconnecting in %s", msg, backoff))
		m.update(e, func(s *Status) {
			*s = Status{ID: t.ID, State: StateReconnecting, Error: msg, Attempt: attempt}
		})
		select {
		case <-ctx.Done():
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: StateStopped} })
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, m.deps.BackoffMax)
	}
}

// runOnce spawns the process and returns when it has exited. wasRunning says whether it became ready.
func (m *Manager) runOnce(ctx context.Context, t model.Tunnel, spec command.Spec, e *entry, attempt int) (wasRunning bool, err error) {
	path, err := m.deps.Resolve(spec.Tool)
	if err != nil {
		e.log.Note(err.Error())
		return false, err
	}
	env, release, err := m.environment(t, spec)
	if err != nil {
		e.log.Note(err.Error())
		return false, err
	}
	defer release()

	// Readiness probes these ports, so a listener that is already there would read as success.
	for _, addr := range spec.Ready.Ports {
		if allAccepting(ctx, []string{addr}) {
			err := fmt.Errorf("%s is already in use", addr)
			e.log.Note(err.Error())
			return false, err
		}
	}

	readyLine := make(chan string, 1)
	if re := spec.Ready.LogPattern; re != nil {
		e.log.setOnLine(lineMatcher(re, readyLine))
	} else {
		e.log.setOnLine(nil)
	}
	e.log.Note("starting: " + spec.Preview())
	proc, err := m.deps.Runner.Start(SpawnRequest{Path: path, Args: spec.Args, Dir: spec.Dir, Env: env, Output: e.log})
	if err != nil {
		e.log.Note(err.Error())
		return false, err
	}
	if m.deps.Records != nil {
		if err := m.deps.Records.Add(Record{TunnelID: t.ID, PID: proc.PID(), Identity: proc.Identity()}); err != nil {
			e.log.Note("cannot record process: " + err.Error())
		}
		defer m.deps.Records.Remove(proc.PID())
	}
	m.update(e, func(s *Status) {
		*s = Status{ID: t.ID, State: StateStarting, PID: proc.PID(), Attempt: attempt}
	})

	exited := make(chan error, 1)
	go func() { exited <- proc.Wait() }()

	readyCtx, cancelReady := context.WithTimeout(ctx, m.deps.ReadyTimeout)
	defer cancelReady()
	ready := make(chan string, 1)
	go m.waitReady(readyCtx, spec.Ready, readyLine, ready)

	for {
		select {
		case <-ctx.Done():
			proc.Stop(m.deps.StopGrace)
			<-exited
			e.log.Note("stopped")
			return wasRunning, nil
		case err := <-exited:
			if err != nil {
				e.log.Note("exited: " + err.Error())
			} else {
				e.log.Note("exited")
			}
			return wasRunning, err
		case addr := <-ready:
			ready = nil
			wasRunning = true
			if addr == "" {
				addr = defaultAddress(t)
			}
			m.update(e, func(s *Status) {
				*s = Status{ID: t.ID, State: StateRunning, PID: proc.PID(), StartedAt: time.Now(), Address: addr}
			})
		case <-readyTimedOut(readyCtx, ctx, wasRunning):
			if ctx.Err() != nil {
				continue // stopped, not timed out; the next pass takes the ctx.Done branch
			}
			e.log.Note(fmt.Sprintf("not ready after %s", m.deps.ReadyTimeout))
			proc.Stop(m.deps.StopGrace)
			<-exited
			return false, fmt.Errorf("not ready after %s", m.deps.ReadyTimeout)
		}
	}
}

// readyTimedOut fires when the readiness deadline passes before the tunnel became ready.
func readyTimedOut(readyCtx, ctx context.Context, wasRunning bool) <-chan struct{} {
	if wasRunning || ctx.Err() != nil {
		return nil
	}
	return readyCtx.Done()
}

func (m *Manager) environment(t model.Tunnel, spec command.Spec) ([]string, func(), error) {
	env := m.deps.Environ()
	for name, secretName := range spec.SecretEnv {
		value, err := m.deps.Secrets.Get(secretName)
		if err != nil {
			return nil, nil, fmt.Errorf("secret %q: %w", secretName, err)
		}
		env = append(env, name+"="+value)
	}
	release := func() {}
	if spec.Askpass && m.deps.Askpass != nil {
		extra, rel := m.deps.Askpass.Register(t.ID, spec.Passphrase)
		env = append(env, extra...)
		release = rel
	}
	return env, release, nil
}

// waitReady reports readiness at most once, along with the address found in the log, if any.
func (m *Manager) waitReady(ctx context.Context, r command.Ready, line <-chan string, ready chan<- string) {
	switch {
	case r.LogPattern != nil:
		select {
		case <-ctx.Done():
		case addr := <-line:
			ready <- addr
		}
	case len(r.Ports) > 0:
		ticker := time.NewTicker(m.deps.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if allAccepting(ctx, r.Ports) {
					ready <- ""
					return
				}
			}
		}
	default:
		select {
		case <-ctx.Done():
		case <-time.After(r.AliveFor):
			ready <- ""
		}
	}
}

func allAccepting(ctx context.Context, addrs []string) bool {
	for _, addr := range addrs {
		dialCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", dialAddress(addr))
		cancel()
		if err != nil {
			return false
		}
		conn.Close()
	}
	return true
}

// dialAddress accepts the forms ssh accepts for a bind address: "port" and "host:port".
func dialAddress(bind string) string {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return net.JoinHostPort("127.0.0.1", bind)
	}
	if host == "" || host == "*" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

var (
	urlInLog    = regexp.MustCompile(`https?://[^\s|"']+`)
	logfmtField = regexp.MustCompile(`\burl=(\S+)`) // logfmt lines hold several URLs; url= is the public one
)

// lineMatcher signals the first line matching re, with the URL it contains, if any.
func lineMatcher(re *regexp.Regexp, found chan<- string) func(string) {
	var once sync.Once
	return func(line string) {
		if !re.MatchString(line) {
			return
		}
		once.Do(func() {
			if m := logfmtField.FindStringSubmatch(line); m != nil {
				found <- m[1]
				return
			}
			found <- urlInLog.FindString(line)
		})
	}
}

func defaultAddress(t model.Tunnel) string {
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardRemote {
			return f.Bind
		}
	}
	return ""
}
