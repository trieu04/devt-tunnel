package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func lifecycleRun(ctx context.Context, args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	code := Run(ctx, append([]string{"--no-daemon-start"}, args...), IO{In: strings.NewReader(""), Out: &out, Err: &diagnostics}, Options{Version: "test"})
	return code, out.String(), diagnostics.String()
}

func TestLifecycleStartIdempotentAndQuiet(t *testing.T) {
	var starts, waits atomic.Int32
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case "tunnel.get":
			return fixtureTunnels()[0], nil
		case api.EventsSubscribe, "prompt.list":
			if method == "prompt.list" {
				return []api.PendingPrompt{}, nil
			}
			return api.Empty{}, nil
		case "tunnel.start":
			starts.Add(1)
			return api.Empty{}, nil
		case "tunnel.waitReady":
			waits.Add(1)
			return fixtureTunnels()[0].Status, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	for _, args := range [][]string{
		{"start", "db-prod"},
		{"start", "Production DB", "db-p", "--json"},
		{"start", "db-prod", "--quiet"},
		{"start", "db-prod", "--no-wait"},
	} {
		code, out, diagnostics := lifecycleRun(context.Background(), args...)
		if code != 0 || diagnostics != "" {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
		switch {
		case slices.Contains(args, "--quiet"):
			if out != "" {
				t.Errorf("quiet stdout=%q", out)
			}
		case slices.Contains(args, "--json"):
			var result struct {
				APIVersion int `json:"apiVersion"`
				Tunnels    []struct{ ID, State string }
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.APIVersion != api.Version || len(result.Tunnels) != 1 || result.Tunnels[0].State != "running" {
				t.Errorf("JSON result=%+v err=%v", result, err)
			}
		default:
			if out != "127.0.0.1:15432\n" {
				t.Errorf("stdout=%q", out)
			}
		}
	}
	if starts.Load() != 4 || waits.Load() != 3 {
		t.Fatalf("starts=%d waits=%d", starts.Load(), waits.Load())
	}
}

func TestLifecycleStopAllAndGroup(t *testing.T) {
	var stopped []string
	var groupStops []string
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		var p api.IDParams
		_ = json.Unmarshal(params, &p)
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case "tunnel.get":
			for _, view := range fixtureTunnels() {
				if view.Tunnel.ID == p.ID {
					view.Status.State = supervisor.StateStopped
					return view, nil
				}
			}
		case "tunnel.stop":
			stopped = append(stopped, p.ID)
			return api.Empty{}, nil
		case "group.list":
			return []model.Group{{ID: "work", Name: "Work Group"}}, nil
		case "group.stop":
			groupStops = append(groupStops, p.ID)
			return api.Empty{}, nil
		}
		return nil, api.NewError(api.Internal, "unexpected "+method)
	})
	if code, out, diagnostics := lifecycleRun(context.Background(), "stop", "--all", "--json"); code != 0 || !strings.Contains(out, `"state":"stopped"`) || diagnostics != "" {
		t.Fatalf("stop all: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	if !slices.Equal(stopped, []string{"db-prod", "db-test", "web"}) {
		t.Fatalf("stopped=%v", stopped)
	}
	if code, out, diagnostics := lifecycleRun(context.Background(), "stop", "--group", "Work Group"); code != 0 || out != "db-prod stopped\n" || diagnostics != "" {
		t.Fatalf("group stop: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	if !slices.Equal(groupStops, []string{"work"}) || len(stopped) != 3 {
		t.Fatalf("groupStops=%v stopped=%v", groupStops, stopped)
	}
}

func TestLifecycleValidatesBeforeMutation(t *testing.T) {
	var mutations atomic.Int32
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		if method == "tunnel.list" {
			return fixtureTunnels(), nil
		}
		mutations.Add(1)
		return api.Empty{}, nil
	})
	for _, args := range [][]string{
		{"start", "db-prod", "missing"}, {"restart", "db-prod", "missing"},
		{"stop", "db-prod", "missing"}, {"stop"}, {"stop", "--all", "db-prod"},
		{"stop", "--all", "--group", "work"}, {"stop", "--group", ""},
		{"start"}, {"wait", "db-prod", "web"},
	} {
		if code, _, diagnostics := lifecycleRun(context.Background(), args...); code != 2 {
			t.Errorf("%v: code=%d stderr=%s", args, code, diagnostics)
		}
	}
	if mutations.Load() != 0 {
		t.Fatalf("invalid requests mutated daemon %d times", mutations.Load())
	}
}

func TestLifecycleRestartAndWait(t *testing.T) {
	var operations []string
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		operations = append(operations, method)
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case "tunnel.get":
			return fixtureTunnels()[0], nil
		case "tunnel.waitReady":
			return fixtureTunnels()[0].Status, nil
		case "prompt.list":
			return []api.PendingPrompt{}, nil
		case "tunnel.restart", api.EventsSubscribe:
			return api.Empty{}, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	for _, verb := range []string{"restart", "wait"} {
		operations = nil
		if code, out, diagnostics := lifecycleRun(context.Background(), verb, "db-prod"); code != 0 || out != "127.0.0.1:15432\n" || diagnostics != "" {
			t.Fatalf("%s: code=%d stdout=%s stderr=%s", verb, code, out, diagnostics)
		}
		if !slices.Contains(operations, "tunnel.waitReady") || slices.Contains(operations, "tunnel.start") || slices.Contains(operations, "tunnel.restart") != (verb == "restart") {
			t.Errorf("%s operations=%v", verb, operations)
		}
	}
}

func TestLifecycleFailureExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		kind          api.ErrorKind
		message, hint string
		code          int
	}{
		{"missing-secret", api.NeedsSecret, "tunnel needs a secret", "devt-tunnel secret set db-key", 3},
		{"keyring", api.KeyringUnavailable, "the OS keychain is unavailable", "", 3},
		{"bind", api.PortInUse, "bind failed", "", 5},
		{"typed-conflict", api.Conflict, "conflict", "", 5},
		{"invalid-spec", api.InvalidSpec, "invalid forward", "", 2},
		{"timeout", api.Timeout, "not ready", "", 6},
		{"missing-binary", api.NotFound, "missing executable", "", 8},
		{"untyped-bind-message", api.Internal, "127.0.0.1:15432 is already in use", "", 1},
		{"untyped-missing-binary-message", api.Internal, "ngrok was not found on this machine", "", 1},
		{"needs-user", api.NeedsUser, "SSH question pending", "devt-tunnel prompts", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
				switch method {
				case "tunnel.list":
					views := fixtureTunnels()
					views[0].MissingSecrets = []string{"db-key"}
					return views, nil
				case api.EventsSubscribe, "tunnel.start":
					return api.Empty{}, nil
				case "prompt.list":
					return []api.PendingPrompt{}, nil
				case "tunnel.waitReady":
					return nil, api.NewError(tc.kind, tc.message)
				default:
					return nil, api.NewError(api.Internal, "unexpected "+method)
				}
			})
			code, out, diagnostics := lifecycleRun(context.Background(), "start", "db-prod", "--json")
			if code != tc.code || out != "" || !strings.Contains(diagnostics, tc.message) || !strings.Contains(diagnostics, tc.hint) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
			}
			if tc.hint != "" && !strings.Contains(diagnostics, `"hint":"`+tc.hint+`"`) {
				t.Errorf("missing structured hint: %s", diagnostics)
			}
		})
	}
}

func TestLifecycleNoWaitMissingSecret(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		view := fixtureTunnels()[1]
		view.MissingSecrets = []string{"db-key"}
		switch method {
		case "tunnel.list":
			return []api.TunnelView{view}, nil
		case "tunnel.get":
			return view, nil
		case "tunnel.start":
			return api.Empty{}, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	if code, _, diagnostics := lifecycleRun(context.Background(), "start", "db-test", "--no-wait"); code != 3 || !strings.Contains(diagnostics, "devt-tunnel secret set db-key") {
		t.Fatalf("code=%d stderr=%s", code, diagnostics)
	}
}

func TestLifecyclePendingPromptDoesNotHang(t *testing.T) {
	for _, verb := range []string{"start", "restart", "wait"} {
		t.Run(verb, func(t *testing.T) {
			fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
				switch method {
				case "tunnel.list":
					return fixtureTunnels(), nil
				case api.EventsSubscribe, "tunnel.start", "tunnel.restart":
					return api.Empty{}, nil
				case "prompt.list":
					return []api.PendingPrompt{{ID: "question", Prompt: askpass.Prompt{TunnelID: "db-prod", Kind: askpass.KindHostKey, Text: "Continue connecting?"}}}, nil
				default:
					return nil, api.NewError(api.Internal, "should not wait or answer: "+method)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			code, out, diagnostics := lifecycleRun(ctx, verb, "db-prod", "--no-input", "--json")
			if code != 4 || ctx.Err() != nil || out != "" || !strings.Contains(diagnostics, `"hint":"devt-tunnel prompts"`) {
				t.Fatalf("code=%d stdout=%s stderr=%s ctx=%v", code, out, diagnostics, ctx.Err())
			}
		})
	}
}

func TestLifecycleIgnoresOtherTunnelPrompts(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case "tunnel.get":
			return fixtureTunnels()[0], nil
		case api.EventsSubscribe, "tunnel.start":
			return api.Empty{}, nil
		case "tunnel.waitReady":
			return fixtureTunnels()[0].Status, nil
		case "prompt.list":
			return []api.PendingPrompt{{ID: "other", Prompt: askpass.Prompt{TunnelID: "web", Kind: askpass.KindPassword}}}, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	if code, _, diagnostics := lifecycleRun(context.Background(), "start", "db-prod"); code != 0 || diagnostics != "" {
		t.Fatalf("code=%d stderr=%s", code, diagnostics)
	}
}

// This fixture handles RPCs concurrently (as the daemon does), so a pending
// waitReady call does not prevent prompt.list or notifications from being served.
func lifecycleEventDaemon(t *testing.T, fixture func(context.Context, string, json.RawMessage) (any, *api.Error)) func(api.Event) {
	t.Helper()
	dir, err := os.MkdirTemp("", "devt-tunnel-cli-events-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := platform.PreparePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "ctl.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DTT_SOCKET", socket)
	type peer struct {
		conn net.Conn
		mu   sync.Mutex
	}
	var mu sync.Mutex
	var peers []*peer
	send := func(p *peer, value any) {
		p.mu.Lock()
		defer p.mu.Unlock()
		_ = json.NewEncoder(p.conn).Encode(value)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, p := range peers {
			_ = p.conn.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p := &peer{conn: conn}
			mu.Lock()
			peers = append(peers, p)
			mu.Unlock()
			go func() {
				defer conn.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				decoder := json.NewDecoder(conn)
				for {
					var req api.Request
					if decoder.Decode(&req) != nil {
						return
					}
					go func() {
						var result any
						var rpcErr *api.Error
						if req.Method == api.DaemonHello {
							result = api.HelloResult{APIVersion: api.Version, DaemonVersion: "test", PID: 123}
						} else {
							result, rpcErr = fixture(ctx, req.Method, req.Params)
						}
						response := api.Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
						if rpcErr == nil {
							response.Result, _ = json.Marshal(result)
						}
						send(p, response)
					}()
				}
			}()
		}
	}()
	return func(event api.Event) {
		params, _ := json.Marshal(event)
		mu.Lock()
		defer mu.Unlock()
		for _, p := range peers {
			send(p, api.Request{JSONRPC: "2.0", Method: event.Type, Params: params})
		}
	}
}

func TestLifecyclePromptArrivesDuringWait(t *testing.T) {
	waitStarted := make(chan struct{})
	var pending atomic.Bool
	var lists atomic.Int32
	emit := lifecycleEventDaemon(t, func(ctx context.Context, method string, _ json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case api.EventsSubscribe, "tunnel.start":
			return api.Empty{}, nil
		case "prompt.list":
			lists.Add(1)
			if pending.Load() {
				return []api.PendingPrompt{{ID: "new", Prompt: askpass.Prompt{TunnelID: "db-prod", Kind: askpass.KindHostKey}}}, nil
			}
			return []api.PendingPrompt{}, nil
		case "tunnel.waitReady":
			close(waitStarted)
			<-ctx.Done()
			return nil, api.NewError(api.Stopped, "client disconnected")
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan string, 1)
	go func() {
		code, out, diagnostics := lifecycleRun(ctx, "start", "db-prod", "--json")
		if code != 4 || out != "" || !strings.Contains(diagnostics, "devt-tunnel prompts") {
			done <- fmt.Sprintf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
			return
		}
		done <- ""
	}()
	select {
	case <-waitStarted:
	case <-ctx.Done():
		t.Fatal("readiness wait did not start")
	}
	pending.Store(true)
	emit(api.Event{Type: api.EventPromptAdded})
	select {
	case failure := <-done:
		if failure != "" {
			t.Fatal(failure)
		}
	case <-ctx.Done():
		t.Fatal("pending question hung instead of exiting 4")
	}
	if lists.Load() < 2 {
		t.Fatalf("prompt.list calls=%d", lists.Load())
	}
}

func TestLifecycleWaitTimeout(t *testing.T) {
	lifecycleEventDaemon(t, func(ctx context.Context, method string, _ json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.list":
			return fixtureTunnels(), nil
		case api.EventsSubscribe:
			return api.Empty{}, nil
		case "prompt.list":
			return []api.PendingPrompt{}, nil
		case "tunnel.waitReady":
			<-ctx.Done()
			return nil, api.NewError(api.Stopped, "client disconnected")
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if code, out, diagnostics := lifecycleRun(ctx, "wait", "db-prod", "--timeout", "30ms", "--json"); code != 6 || out != "" || !strings.Contains(diagnostics, `"code":"timeout"`) || ctx.Err() != nil {
		t.Fatalf("code=%d stdout=%s stderr=%s ctx=%v", code, out, diagnostics, ctx.Err())
	}
}
