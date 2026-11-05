package engine

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type portProcess struct {
	*fakeProcess
	listener net.Listener
}

func (p *portProcess) Wait() error                { err := p.fakeProcess.Wait(); p.listener.Close(); return err }
func (p *portProcess) Stop(d time.Duration) error { p.listener.Close(); return p.fakeProcess.Stop(d) }

type allocatingRunner struct {
	mu          sync.Mutex
	failures    int
	message     string
	runtimePath string
	requests    chan supervisor.SpawnRequest
	checked     chan error
}

func (r *allocatingRunner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	var bind string
	for i, arg := range req.Args {
		if arg == "-L" && i+1 < len(req.Args) {
			parts := strings.Split(req.Args[i+1], ":")
			bind = parts[0] + ":" + parts[1]
		}
	}
	st, err := (corestate.RuntimeStore{Path: r.runtimePath}).Load()
	if err == nil && (len(st.Ephemeral) != 1 || len(st.Leases) != 1 || st.Ephemeral[0].Forwards[0].Bind != bind) {
		err = fmt.Errorf("retry spawned before durable port update: bind=%s runtime=%+v", bind, st)
	}
	r.checked <- err
	r.requests <- req
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	fail := r.failures > 0
	if fail {
		r.failures--
	}
	r.mu.Unlock()
	if fail {
		return nil, errors.New(r.message)
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}
	return &portProcess{fakeProcess: &fakeProcess{exit: make(chan error, 1)}, listener: listener}, nil
}
func TestAutoPortRetriesForwardFailureAtMostThreeTimes(t *testing.T) {
	for _, tc := range []struct {
		name               string
		failures, attempts int
		message            string
		success            bool
	}{
		{"race then succeeds", 2, 3, "bind: Address already in use", true},
		{"three retries only", 20, 4, "Could not request local forwarding", false},
		{"unrelated failure not retried", 20, 1, "Permission denied (publickey)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &allocatingRunner{failures: tc.failures, message: tc.message, requests: make(chan supervisor.SpawnRequest, 16), checked: make(chan error, 16)}
			e, _, _ := ownedEngine(t, func(o *Options) {
				o.Runner = runner
				o.PortRange = "23000-23020"
				runner.runtimePath = filepath.Join(o.StateDir, "runtime.json")
			}, model.Config{})
			// Use the real ssh builder and readiness probe with a fake runner that binds
			// its allocated port, so stale/fingerprint and readiness behavior are checked.
			a, s := session(t, e, "agent")
			result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: autoTunnel()})
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for i := 0; i < tc.attempts; i++ {
				req := receive(t, runner.requests)
				if err := receive(t, runner.checked); err != nil {
					t.Fatal(err)
				}
				var forward string
				for j, arg := range req.Args {
					if arg == "-L" {
						forward = req.Args[j+1]
					}
				}
				if seen[forward] {
					t.Fatalf("retried already attempted port: %s", forward)
				}
				seen[forward] = true
				if !strings.Contains(strings.Join(req.Args, " "), "ExitOnForwardFailure=yes") {
					t.Fatalf("missing forward failure option: %+v", req.Args)
				}
			}
			if tc.success {
				readyOwned(t, e, result.ID)
				for _, v := range e.Overview().Tunnels {
					if v.Tunnel.ID == result.ID && v.Status.Stale {
						t.Fatal("retry left fingerprint stale")
					}
				}
			} else {
				deadline := time.After(5 * time.Second)
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for e.known(result.ID) {
					select {
					case <-deadline:
						t.Fatal("failed tunnel not removed")
					case <-ticker.C:
					}
				}
			}
			select {
			case req := <-runner.requests:
				t.Fatalf("unexpected extra attempt: %+v", req)
			default:
			}
		})
	}
}
func TestAutoPortRejectsRemoteAndAmbiguousRequests(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, model.Config{})
	a, s := session(t, e, "agent")
	for _, tc := range []struct {
		name    string
		forward model.Forward
	}{
		{"remote", model.Forward{Kind: model.ForwardRemote, ListenPort: "auto", Target: "80"}},
		{"explicit bind plus auto", model.Forward{Kind: model.ForwardLocal, Bind: "0.0.0.0:23456", ListenPort: "auto", Target: "80"}},
		{"bad port", model.Forward{Kind: model.ForwardLocal, ListenPort: "0", Target: "80"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tunnel := autoTunnel()
			tunnel.Forwards = []model.Forward{tc.forward}
			if _, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel}); err == nil {
				t.Fatal("accepted invalid port request")
			}
		})
	}
}
func TestAutoPortReservesLaterConcreteForwards(t *testing.T) {
	e, _, _ := ownedEngine(t, func(o *Options) { o.PortRange = "24000-24003"; o.Listening = func(string) bool { return false } }, model.Config{})
	a, s := session(t, e, "agent")
	tunnel := autoTunnel()
	tunnel.Forwards = append(tunnel.Forwards, model.Forward{Kind: model.ForwardDynamic, ListenPort: "24000"})
	result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel})
	if err != nil {
		t.Fatal(err)
	}
	for _, tunnel := range e.manager.Config().Tunnels {
		if tunnel.ID == result.ID {
			if tunnel.Forwards[0].Bind != "127.0.0.1:24001" || tunnel.Forwards[1].Bind != "127.0.0.1:24000" {
				t.Fatalf("forwards=%+v", tunnel.Forwards)
			}
		}
	}
}
func TestPortRangeValidationAndDefault(t *testing.T) {
	for _, value := range []string{"0-20", "20-10", "1-65536", "bad", "20000", "20000-20001-20002"} {
		if _, _, err := parsePortRange(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	e, _, _ := ownedEngine(t, nil, model.Config{})
	if e.portFirst != 20000 || e.portLast != 20999 {
		t.Fatalf("default range %d-%d", e.portFirst, e.portLast)
	}
	a, b, err := parsePortRange("20000-20999")
	if err != nil || strconv.Itoa(a)+"-"+strconv.Itoa(b) != "20000-20999" {
		t.Fatal(a, b, err)
	}
}
