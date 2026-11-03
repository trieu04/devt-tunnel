package supervisor

import (
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type fakeProcess struct {
	pid     int
	req     SpawnRequest
	exit    chan error
	once    sync.Once
	mu      sync.Mutex
	stopped bool
}

func (p *fakeProcess) PID() int         { return p.pid }
func (p *fakeProcess) Identity() string { return fmt.Sprintf("id-%d", p.pid) }
func (p *fakeProcess) Wait() error      { return <-p.exit }
func (p *fakeProcess) Stop(time.Duration) error {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	p.finish(nil)
	return nil
}
func (p *fakeProcess) finish(err error) {
	p.once.Do(func() { p.exit <- err })
}
func (p *fakeProcess) wasStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

type fakeRunner struct {
	mu      sync.Mutex
	procs   []*fakeProcess
	err     error
	onStart func(*fakeProcess)
}

func (r *fakeRunner) Start(req SpawnRequest) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	p := &fakeProcess{pid: 100 + len(r.procs), req: req, exit: make(chan error, 1)}
	r.procs = append(r.procs, p)
	if r.onStart != nil {
		go r.onStart(p)
	}
	return p, nil
}

func (r *fakeRunner) proc(t *testing.T, i int) *fakeProcess {
	t.Helper()
	var p *fakeProcess
	eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		if i < len(r.procs) {
			p = r.procs[i]
			return true
		}
		return false
	}, "process %d to start", i)
	return p
}

func eventually(t *testing.T, cond func() bool, format string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for "+format, args...)
}

func newManager(r *fakeRunner, store secret.Store, tunnels ...model.Tunnel) *Manager {
	m := New(Deps{
		Runner:       r,
		Secrets:      store,
		Resolve:      func(tool string) (string, error) { return "/bin/" + tool, nil },
		Environ:      func() []string { return []string{"PATH=/bin"} },
		Shell:        []string{"/bin/sh", "-c"},
		BackoffBase:  5 * time.Millisecond,
		BackoffMax:   20 * time.Millisecond,
		PollInterval: 2 * time.Millisecond,
	})
	m.SetConfig(model.Config{Tunnels: tunnels})
	return m
}

func logReady(id string, reconnect bool) model.Tunnel {
	return model.Tunnel{ID: id, Name: id, Type: model.TypeCommand, Reconnect: reconnect,
		Command: &model.Command{Line: "run", ReadyLog: "ready"}}
}

func state(m *Manager, id string) Status {
	for _, s := range m.Statuses() {
		if s.ID == id {
			return s
		}
	}
	return Status{}
}

func waitState(t *testing.T, m *Manager, id string, want State) Status {
	t.Helper()
	eventually(t, func() bool { return state(m, id).State == want }, "state %s, have %s", want, state(m, id).State)
	return state(m, id)
}

func TestReadyFromLogExtractsAddress(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", false))
	if err := m.Start("a"); err != nil {
		t.Fatal(err)
	}
	p := r.proc(t, 0)
	waitState(t, m, "a", StateStarting)
	io.WriteString(p.req.Output, "booting\nready at https://x.example.com/ now\n")
	st := waitState(t, m, "a", StateRunning)
	if st.Address != "https://x.example.com/" || st.PID != p.pid {
		t.Errorf("status %+v", st)
	}
	m.Stop("a")
	if st := state(m, "a"); st.State != StateStopped || !p.wasStopped() {
		t.Errorf("after stop: %+v stopped=%v", st, p.wasStopped())
	}
}

func TestReadyFromLogfmtPrefersPublicURL(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", false))
	m.Start("a")
	p := r.proc(t, 0)
	io.WriteString(p.req.Output, "msg=ready addr=http://127.0.0.1:1 url=https://public.example.com\n")
	if st := waitState(t, m, "a", StateRunning); st.Address != "https://public.example.com" {
		t.Errorf("address %q", st.Address)
	}
	m.Stop("a")
}

func portTunnel(addr string) model.Tunnel {
	return model.Tunnel{ID: "s", Type: model.TypeSSH, SSH: &model.SSH{Host: "h"},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: addr, Target: "x:1"}}}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestReadyWhenPortsAccept(t *testing.T) {
	addr := freeAddr(t)
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, portTunnel(addr))
	m.Start("s")
	r.proc(t, 0)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if st := waitState(t, m, "s", StateRunning); st.Address != addr {
		t.Errorf("address %q", st.Address)
	}
	m.Stop("s")
}

func TestPortAlreadyInUseFailsWithoutSpawning(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, portTunnel(l.Addr().String()))
	m.Start("s")
	if st := waitState(t, m, "s", StateFailed); !strings.Contains(st.Error, "already in use") {
		t.Errorf("status %+v", st)
	}
	if len(r.procs) != 0 {
		t.Errorf("spawned %d", len(r.procs))
	}
}

func TestPortsNotAcceptingStaysStarting(t *testing.T) {
	addr := freeAddr(t)
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, model.Tunnel{ID: "s", Type: model.TypeSSH,
		SSH:      &model.SSH{Host: "h"},
		Forwards: []model.Forward{{Kind: model.ForwardDynamic, Bind: addr}}})
	m.Start("s")
	r.proc(t, 0)
	time.Sleep(30 * time.Millisecond)
	if st := state(m, "s"); st.State != StateStarting {
		t.Errorf("state %s", st.State)
	}
	m.Stop("s")
}

func TestFirstStartFailureIsFailedEvenWithReconnect(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", true))
	m.Start("a")
	r.proc(t, 0).finish(errors.New("exit status 255"))
	if st := waitState(t, m, "a", StateFailed); st.Error != "exit status 255" {
		t.Errorf("status %+v", st)
	}
	if len(r.procs) != 1 {
		t.Errorf("spawned %d times", len(r.procs))
	}
}

func TestReconnectAfterRunning(t *testing.T) {
	r := &fakeRunner{onStart: func(p *fakeProcess) { io.WriteString(p.req.Output, "ready\n") }}
	m := newManager(r, &secret.Memory{}, logReady("a", true))
	m.Start("a")
	first := r.proc(t, 0)
	waitState(t, m, "a", StateRunning)
	first.finish(errors.New("connection lost"))
	second := r.proc(t, 1)
	eventually(t, func() bool { return state(m, "a").PID == second.pid && state(m, "a").State == StateRunning }, "reconnected")
	m.Stop("a")
	waitState(t, m, "a", StateStopped)
}

func TestReconnectRetriesFailedAttempts(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", true))
	m.Start("a")
	first := r.proc(t, 0)
	io.WriteString(first.req.Output, "ready\n")
	waitState(t, m, "a", StateRunning)
	first.finish(errors.New("lost"))
	// Attempts that die before becoming ready keep retrying once the tunnel has run.
	r.proc(t, 1).finish(errors.New("refused"))
	r.proc(t, 2).finish(errors.New("refused"))
	r.proc(t, 3)
	m.Stop("a")
	waitState(t, m, "a", StateStopped)
}

func TestNoReconnectWhenDisabled(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", false))
	m.Start("a")
	p := r.proc(t, 0)
	io.WriteString(p.req.Output, "ready\n")
	waitState(t, m, "a", StateRunning)
	p.finish(nil)
	waitState(t, m, "a", StateFailed)
}

func TestStopDuringBackoff(t *testing.T) {
	r := &fakeRunner{}
	m := New(Deps{Runner: r, Secrets: &secret.Memory{},
		Resolve: func(tool string) (string, error) { return tool, nil },
		Environ: func() []string { return nil }, Shell: []string{"sh", "-c"},
		BackoffBase: time.Hour, BackoffMax: time.Hour})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{logReady("a", true)}})
	m.Start("a")
	p := r.proc(t, 0)
	io.WriteString(p.req.Output, "ready\n")
	waitState(t, m, "a", StateRunning)
	p.finish(errors.New("lost"))
	waitState(t, m, "a", StateReconnecting)
	m.Stop("a")
	if st := state(m, "a"); st.State != StateStopped {
		t.Errorf("state %s", st.State)
	}
}

func TestReadyTimeoutFailsAndStops(t *testing.T) {
	r := &fakeRunner{}
	m := New(Deps{Runner: r, Secrets: &secret.Memory{},
		Resolve: func(tool string) (string, error) { return tool, nil },
		Environ: func() []string { return nil }, Shell: []string{"sh", "-c"},
		ReadyTimeout: 10 * time.Millisecond})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{logReady("a", false)}})
	m.Start("a")
	p := r.proc(t, 0)
	st := waitState(t, m, "a", StateFailed)
	if !strings.Contains(st.Error, "not ready") || !p.wasStopped() {
		t.Errorf("status %+v stopped=%v", st, p.wasStopped())
	}
}

func secretTunnel() model.Tunnel {
	return model.Tunnel{ID: "n", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "1", Secret: "tok"}}
}

func TestMissingSecretNeedsTokenAndDoesNotSpawn(t *testing.T) {
	r := &fakeRunner{}
	store := &secret.Memory{}
	m := newManager(r, store, secretTunnel())
	if st := state(m, "n"); st.State != StateNeedsToken {
		t.Errorf("at rest: %s", st.State)
	}
	m.Start("n")
	if len(r.procs) != 0 {
		t.Error("spawned without secret")
	}
	store.Set("tok", "VALUE")
	m.Start("n")
	p := r.proc(t, 0)
	if !contains(p.req.Env, "NGROK_AUTHTOKEN=VALUE") {
		t.Errorf("env %v", p.req.Env)
	}
	for _, a := range p.req.Args {
		if strings.Contains(a, "VALUE") {
			t.Errorf("secret in argv: %v", p.req.Args)
		}
	}
	m.Stop("n")
	logs := strings.Join(m.Logs("n"), "\n")
	if strings.Contains(logs, "VALUE") {
		t.Errorf("secret in logs: %s", logs)
	}
}

type brokenStore struct{ secret.Memory }

func (*brokenStore) Get(string) (string, error) { return "", secret.ErrUnavailable }

func TestKeyringUnavailableState(t *testing.T) {
	m := newManager(&fakeRunner{}, &brokenStore{}, secretTunnel())
	if st := state(m, "n"); st.State != StateKeyringUnavailable {
		t.Errorf("state %s", st.State)
	}
}

func TestToolNotFoundFails(t *testing.T) {
	r := &fakeRunner{}
	m := New(Deps{Runner: r, Secrets: &secret.Memory{},
		Resolve: func(tool string) (string, error) { return "", fmt.Errorf("%s not found", tool) },
		Environ: func() []string { return nil }})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{{ID: "s", Type: model.TypeSSH, SSH: &model.SSH{Host: "h"}}}})
	m.Start("s")
	if st := waitState(t, m, "s", StateFailed); !strings.Contains(st.Error, "ssh not found") {
		t.Errorf("status %+v", st)
	}
}

type fakeAskpass struct {
	mu       sync.Mutex
	secret   string
	released int
}

func (a *fakeAskpass) Register(id, s string) ([]string, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.secret = s
	return []string{"SSH_ASKPASS=/helper"}, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.released++
	}
}

func TestSSHUsesAskpassAndReleases(t *testing.T) {
	r := &fakeRunner{}
	a := &fakeAskpass{}
	m := New(Deps{Runner: r, Secrets: &secret.Memory{}, Askpass: a,
		Resolve: func(tool string) (string, error) { return tool, nil },
		Environ: func() []string { return nil }})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{{ID: "s", Type: model.TypeSSH, SSH: &model.SSH{Host: "h"}}}})
	m.Start("s")
	p := r.proc(t, 0)
	if !contains(p.req.Env, "SSH_ASKPASS=/helper") {
		t.Errorf("env %v", p.req.Env)
	}
	m.Stop("s")
	if a.released != 1 {
		t.Errorf("released %d", a.released)
	}
}

func TestRecordsTrackProcessLifetime(t *testing.T) {
	r := &fakeRunner{}
	recs := &FileRecords{Path: filepath.Join(t.TempDir(), "state", "processes.json")}
	m := New(Deps{Runner: r, Secrets: &secret.Memory{}, Records: recs,
		Resolve: func(tool string) (string, error) { return tool, nil },
		Environ: func() []string { return nil }, Shell: []string{"sh", "-c"}})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{logReady("a", false)}})
	m.Start("a")
	p := r.proc(t, 0)
	eventually(t, func() bool { l, _ := recs.List(); return len(l) == 1 }, "record")
	l, _ := recs.List()
	if l[0].PID != p.pid || l[0].TunnelID != "a" || l[0].Identity != p.Identity() {
		t.Errorf("record %+v", l[0])
	}
	m.Stop("a")
	if l, _ := recs.List(); len(l) != 0 {
		t.Errorf("left %+v", l)
	}
}

func TestFindOrphans(t *testing.T) {
	recs := []Record{{PID: 1, Identity: "alive"}, {PID: 2, Identity: "old"}, {PID: 3, Identity: "gone"}}
	got := FindOrphans(recs, func(pid int) (string, error) {
		switch pid {
		case 1:
			return "alive", nil
		case 2:
			return "reused", nil
		}
		return "", errors.New("no such process")
	})
	if len(got) != 1 || got[0].PID != 1 {
		t.Errorf("orphans %+v", got)
	}
}

func TestSetConfigStopsRemovedTunnels(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", false))
	m.Start("a")
	p := r.proc(t, 0)
	m.SetConfig(model.Config{})
	if !p.wasStopped() || len(m.Statuses()) != 0 {
		t.Errorf("stopped=%v statuses=%v", p.wasStopped(), m.Statuses())
	}
}

func TestShutdownStopsAllAndRefusesStart(t *testing.T) {
	r := &fakeRunner{}
	m := newManager(r, &secret.Memory{}, logReady("a", false), logReady("b", false))
	m.Start("a")
	m.Start("b")
	a, b := r.proc(t, 0), r.proc(t, 1)
	m.Shutdown()
	if !a.wasStopped() || !b.wasStopped() {
		t.Error("not all stopped")
	}
	if err := m.Start("a"); err == nil {
		t.Error("start after shutdown should fail")
	}
}

func TestLogRingKeepsTailAndJoinsPartialLines(t *testing.T) {
	r := newLogRing(2)
	io.WriteString(r, "one\ntw")
	io.WriteString(r, "o\r\nthree\n")
	got := r.snapshot()
	if len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Errorf("lines %q", got)
	}
}

func TestDialAddress(t *testing.T) {
	cases := map[string]string{
		"8080":           "127.0.0.1:8080",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"*:8080":         "127.0.0.1:8080",
		"0.0.0.0:8080":   "127.0.0.1:8080",
		"[::1]:8080":     "[::1]:8080",
	}
	for in, want := range cases {
		if got := dialAddress(in); got != want {
			t.Errorf("dialAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
