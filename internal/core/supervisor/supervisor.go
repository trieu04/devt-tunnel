// Package supervisor runs tunnels as child processes and tracks their state.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
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
	Home     string
	Dialect  command.Dialect
	Askpass  Askpass // optional
	Records  Records // optional
	OnChange func()  // optional; called after any state change
	// RetryForward persists an allocated replacement before the next spawn.
	RetryForward func(context.Context, model.Tunnel, error) (model.Tunnel, bool, error)

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
	ID        string            `json:"id"`
	State     State             `json:"state"`
	PID       int               `json:"pid,omitempty"`
	StartedAt time.Time         `json:"startedAt,omitempty"`
	Error     string            `json:"error,omitempty"`
	Address   string            `json:"address,omitempty"`
	Attempt   int               `json:"attempt,omitempty"`
	Stale     bool              `json:"stale,omitempty"`
	Owner     model.TunnelOwner `json:"owner"`
}

// Manager owns all tunnels.
type Manager struct {
	deps Deps

	mu            sync.Mutex
	config        model.Config
	tunnels       map[string]*entry
	closed        bool
	activeProfile string
	pendingStart  map[string]bool
	waiters       map[string][]chan Status
}

type entry struct {
	id          string
	status      Status
	log         *logRing
	cancel      context.CancelFunc
	done        chan struct{}
	fingerprint string
	wake        chan struct{}
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
	return &Manager{deps: deps, tunnels: map[string]*entry{}, pendingStart: map[string]bool{}, waiters: map[string][]chan Status{}}
}

// SetConfig replaces the definitions. Tunnels no longer defined are stopped.
// Running tunnels keep their old settings until they are restarted.
func (m *Manager) SetConfig(cfg model.Config) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
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

// Active reads runtime state only, without the secret-presence checks used for presentation.
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.tunnels {
		switch e.status.State {
		case StateStarting, StateRunning, StateReconnecting:
			return true
		}
	}
	return false
}

// RuntimeStatuses omits keychain presence and derived presentation fields for daemon lifecycle calls.
func (m *Manager) RuntimeStatuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.config.Tunnels))
	for _, t := range m.config.Tunnels {
		if e := m.tunnels[t.ID]; e != nil {
			out = append(out, e.status)
		}
	}
	return out
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
		if out[i].State != StateStopped && out[i].State != StateNeedsToken && out[i].State != StateKeyringUnavailable {
			m.mu.Lock()
			e := m.tunnels[t.ID]
			spec, err := command.Build(t, m.optionsLocked(t))
			out[i].Stale = err == nil && e != nil && e.fingerprint != "" && e.fingerprint != spec.Fingerprint()
			m.mu.Unlock()
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

func (m *Manager) LogsSince(id string, after uint64) (uint64, []LogLine) {
	m.mu.Lock()
	e := m.tunnels[id]
	m.mu.Unlock()
	if e == nil {
		return after, nil
	}
	return e.log.snapshotSince(after)
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
	return spec.Preview(m.deps.Dialect), nil
}

func (m *Manager) PreviewDraft(t model.Tunnel) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	spec, err := command.Build(t, m.optionsLocked(t))
	if err != nil {
		return "", err
	}
	return spec.Preview(m.deps.Dialect), nil
}

// BuildDraft uses exactly the command options used for execution.
func (m *Manager) BuildDraft(t model.Tunnel) (command.Spec, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return command.Build(t, m.optionsLocked(t))
}

// StartFingerprint refuses deferred secret starts and port-changing retries.
// An approval must never silently authorize a different execution spec.
func (m *Manager) StartFingerprint(id, fingerprint string) error {
	return m.start(id, fingerprint)
}

// Start launches a tunnel. Starting an active tunnel does nothing.
func (m *Manager) Start(id string) error { return m.start(id, "") }
func (m *Manager) start(id, fingerprint string) error {
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
		if fingerprint != "" {
			return ErrNeedsSecret
		}
		m.pendingStart[id] = true
		e.status = Status{ID: id, State: StateStopped}
		go m.changed()
		return nil
	}
	spec, err := command.Build(t, m.optionsLocked(t))
	if err != nil {
		e.status = Status{ID: id, State: StateFailed, Error: err.Error()}
		go m.changed()
		return err
	}
	if fingerprint != "" && spec.Fingerprint() != fingerprint {
		return errors.New("approved command fingerprint changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	e.wake = make(chan struct{}, 1)
	e.fingerprint = spec.Fingerprint()
	delete(m.pendingStart, id)
	e.status = Status{ID: id, State: StateStarting}
	go m.loop(ctx, t, spec, e, fingerprint != "")
	go m.changed()
	return nil
}

// Stop ends a tunnel and waits for it to exit.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	delete(m.pendingStart, id)
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

func (m *Manager) Restart(id string) error {
	m.Stop(id)
	return m.Start(id)
}

// SecretsChanged retries only starts that were explicitly requested while a secret was missing.
func (m *Manager) SecretsChanged() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(m.pendingStart))
	for id := range m.pendingStart {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.mu.Lock()
		t, ok := m.findLocked(id)
		m.mu.Unlock()
		if ok && m.secretState(t) == "" {
			_ = m.Start(id)
		}
	}
}

// Wake interrupts reconnect backoff after the system resumes.
func (m *Manager) Wake() {
	m.mu.Lock()
	for _, e := range m.tunnels {
		if e.wake != nil && e.status.State == StateReconnecting {
			select {
			case e.wake <- struct{}{}:
			default:
			}
		}
	}
	m.mu.Unlock()
}

var (
	ErrNeedsSecret = errors.New("tunnel needs a secret")
	ErrStopped     = errors.New("tunnel stopped")
)

func (m *Manager) WaitReady(ctx context.Context, id string) (Status, error) {
	m.mu.Lock()
	e, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("unknown tunnel %q", id)
	}
	if terminalStatus(e.status.State) {
		st := e.status
		err := readyError(st, m.pendingStart[id])
		m.mu.Unlock()
		return st, err
	}
	ch := make(chan Status, 1)
	m.waiters[id] = append(m.waiters[id], ch)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i, waiter := range m.waiters[e.id] {
			if waiter == ch {
				m.waiters[e.id] = append(m.waiters[e.id][:i], m.waiters[e.id][i+1:]...)
				if len(m.waiters[e.id]) == 0 {
					delete(m.waiters, e.id)
				}
				break
			}
		}
	}()
	select {
	case <-ctx.Done():
		return Status{}, ctx.Err()
	case st := <-ch:
		m.mu.Lock()
		err := readyError(st, m.pendingStart[id])
		m.mu.Unlock()
		return st, err
	}
}

func readyError(st Status, pending bool) error {
	switch st.State {
	case StateRunning:
		return nil
	case StateNeedsToken:
		return ErrNeedsSecret
	case StateKeyringUnavailable:
		return secret.ErrUnavailable
	case StateStopped:
		if pending {
			return ErrNeedsSecret
		}
		return ErrStopped
	default:
		if st.Error != "" {
			return errors.New(st.Error)
		}
		return errors.New("tunnel failed")
	}
}

func terminalStatus(state State) bool {
	return state == StateRunning || state == StateFailed || state == StateNeedsToken || state == StateKeyringUnavailable || state == StateStopped
}

// Shutdown stops every tunnel and refuses further starts.
func (m *Manager) Shutdown() {
	m.CloseAdmission()
	m.mu.Lock()
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

// CloseAdmission refuses new manager work without waiting for running tunnels to stop.
func (m *Manager) CloseAdmission() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
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
		e = &entry{id: id, status: Status{ID: id, State: StateStopped}, log: newLogRing(m.deps.LogLines)}
		m.tunnels[id] = e
	}
	return e
}

// optionsLocked picks the SSH config of the tunnel's first profile that sets one.
func (m *Manager) optionsLocked(t model.Tunnel) command.Options {
	opts := command.Options{Shell: m.deps.Shell, Home: m.deps.Home, Dialect: m.deps.Dialect}
	if m.activeProfile != "" {
		for _, pid := range t.Profiles {
			if pid != m.activeProfile {
				continue
			}
			for _, p := range m.config.Profiles {
				if p.ID == pid && p.SSHConfig != "" {
					opts.SSHConfig = p.SSHConfig
					return opts
				}
			}
		}
	}
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

func (m *Manager) SetActiveProfile(id string) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.activeProfile = id
	m.mu.Unlock()
	m.changed()
}

func (m *Manager) secretState(t model.Tunnel) State {
	for _, name := range t.Secrets() {
		if _, err := secret.Has(m.deps.Secrets, name); err != nil {
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
	e.status.ID = e.id
	if terminalStatus(e.status.State) {
		for _, waiter := range m.waiters[e.status.ID] {
			select {
			case waiter <- e.status:
			default:
			}
		}
		delete(m.waiters, e.status.ID)
	}
	m.mu.Unlock()
	m.changed()
}

// loop runs one tunnel from Start until it is stopped or gives up.
func (m *Manager) loop(ctx context.Context, t model.Tunnel, spec command.Spec, e *entry, fingerprintBound bool) {
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
	forwardRetries := 0
	for {
		attempt++
		wasRunning, err := m.runOnce(ctx, t, spec, e, attempt, retrying)
		if ctx.Err() != nil {
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: StateStopped} })
			return
		}
		if errors.Is(err, secret.ErrNotFound) || errors.Is(err, secret.ErrUnavailable) {
			state := StateNeedsToken
			if errors.Is(err, secret.ErrUnavailable) {
				state = StateKeyringUnavailable
			}
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: state, Error: err.Error()} })
			return
		}
		if !fingerprintBound && !wasRunning && forwardRetries < 3 && m.deps.RetryForward != nil && forwardFailure(err) {
			m.mu.Lock()
			t.ID = e.id
			m.mu.Unlock()
			replacement, retry, retryErr := m.deps.RetryForward(ctx, t, err)
			if retryErr != nil {
				err = retryErr
			}
			if retry {
				t = replacement
				m.mu.Lock()
				spec, err = command.Build(t, m.optionsLocked(t))
				if err == nil {
					e.fingerprint = spec.Fingerprint()
				}
				m.mu.Unlock()
				if err == nil {
					forwardRetries++
					continue
				}
			}
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
		displayAttempt := attempt
		if displayAttempt == 0 {
			displayAttempt = 1
		}
		msg := "process exited"
		if err != nil {
			msg = err.Error()
		}
		e.log.Note(fmt.Sprintf("%s; reconnecting in %s", msg, backoff))
		m.update(e, func(s *Status) {
			*s = Status{ID: t.ID, State: StateReconnecting, Error: msg, Attempt: displayAttempt}
		})
		select {
		case <-ctx.Done():
			m.update(e, func(s *Status) { *s = Status{ID: t.ID, State: StateStopped} })
			return
		case <-time.After(backoff):
			backoff = min(backoff*2, m.deps.BackoffMax)
		case <-e.wake:
			backoff = m.deps.BackoffBase
		}
	}
}

// runOnce spawns the process and returns when it has exited. wasRunning says whether it became ready.
func (m *Manager) runOnce(ctx context.Context, t model.Tunnel, spec command.Spec, e *entry, attempt int, retrying bool) (wasRunning bool, err error) {
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
	e.log.Note("starting: " + spec.Preview(m.deps.Dialect))
	proc, err := m.deps.Runner.Start(SpawnRequest{Path: path, Args: spec.Args, Dir: spec.Dir, Env: env, Output: e.log})
	if err != nil {
		e.log.Note(err.Error())
		return false, err
	}
	if m.deps.Records != nil {
		if err := m.addProcessRecord(e, proc); err != nil {
			e.log.Note("cannot record process: " + err.Error())
		}
		defer m.deps.Records.Remove(proc.PID())
	}
	m.update(e, func(s *Status) {
		state := StateStarting
		if retrying {
			state = StateReconnecting
		}
		*s = Status{ID: t.ID, State: state, PID: proc.PID(), Attempt: attempt}
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
			if line := e.log.lastMeaningful(); line != "" {
				return wasRunning, errors.New(line)
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
			if errors.Is(err, secret.ErrNotFound) || errors.Is(err, secret.ErrUnavailable) {
				return nil, nil, fmt.Errorf("secret %q: %w", secretName, err)
			}
			return nil, nil, err
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
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", command.DialAddress(addr))
		cancel()
		if err != nil {
			return false
		}
		conn.Close()
	}
	return true
}

func dialAddress(bind string) string { return command.DialAddress(bind) }

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
	if t.Type == model.TypeCloudflared && t.Cloudflared != nil && t.Cloudflared.Hostname != "" {
		return "https://" + t.Cloudflared.Hostname
	}
	if t.Type == model.TypeNgrok && t.Ngrok != nil {
		return t.Ngrok.Addr
	}
	return ""
}

// Rename keeps the running entry and its fingerprint while changing its registry
// key. ID/name are not part of command.Spec.Fingerprint.
func (m *Manager) Rename(from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.tunnels[from]; e != nil {
		delete(m.tunnels, from)
		m.tunnels[to] = e
		e.id = to
		e.status.ID = to
	}
	m.config.Tunnels = append([]model.Tunnel(nil), m.config.Tunnels...)
	for i := range m.config.Tunnels {
		if m.config.Tunnels[i].ID == from {
			m.config.Tunnels[i].ID = to
		}
	}
	if m.pendingStart[from] {
		delete(m.pendingStart, from)
		m.pendingStart[to] = true
	}
	if waiters := m.waiters[from]; len(waiters) > 0 {
		delete(m.waiters, from)
		m.waiters[to] = append(m.waiters[to], waiters...)
	}
}

func (m *Manager) addProcessRecord(e *entry, proc Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deps.Records.Add(Record{TunnelID: e.id, PID: proc.PID(), Identity: proc.Identity()})
}

func forwardFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, text := range []string{"address already in use", "already in use", "cannot listen to port", "could not request local forwarding", "forwarding failed", "port forwarding failed", "cannot bind"} {
		if strings.Contains(message, text) {
			return true
		}
	}
	return false
}

// WaitingForSecret distinguishes an explicit deferred start from an ordinary
// stop without consulting the keychain.
func (m *Manager) WaitingForSecret(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingStart[id]
}
