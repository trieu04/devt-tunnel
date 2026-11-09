package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type rpcFixture func(method string, params json.RawMessage) (any, *api.Error)

func fakeDaemon(t *testing.T, fixture rpcFixture) {
	t.Helper()
	dir, err := os.MkdirTemp("", "devt-tunnel-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err = platform.PreparePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "ctl.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("DTT_SOCKET", socket)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				decoder := json.NewDecoder(conn)
				for {
					var request api.Request
					if decoder.Decode(&request) != nil {
						return
					}
					var result any
					var rpcErr *api.Error
					if request.Method == api.DaemonHello {
						result = api.HelloResult{APIVersion: api.Version, DaemonVersion: "test", PID: 123}
					} else {
						result, rpcErr = fixture(request.Method, request.Params)
					}
					response := api.Response{JSONRPC: "2.0", ID: request.ID, Error: rpcErr}
					if rpcErr == nil {
						raw, err := json.Marshal(result)
						if err != nil {
							return
						}
						response.Result = raw
					}
					if json.NewEncoder(conn).Encode(response) != nil {
						return
					}
				}
			}()
		}
	}()
}

func readFixture(method string, _ json.RawMessage) (any, *api.Error) {
	switch method {
	case "tunnel.list":
		return fixtureTunnels(), nil
	case "tunnel.findings":
		return []checks.Finding{{Severity: checks.Warning, Message: "test warning"}}, nil
	case "logs.since":
		return api.LogsResult{Sequence: 3, Lines: []supervisor.LogLine{{Seq: 1, Line: "first"}, {Seq: 2, Line: "second"}, {Seq: 3, Line: "third"}}}, nil
	case "ssh.hosts":
		return api.SSHHostList{ConfigPath: "/test/ssh/config", Hosts: []sshconfig.Host{{Alias: "server1", Source: "/test/ssh/config"}}, Warnings: []string{"test SSH warning"}}, nil
	case "ssh.resolve":
		return sshconfig.Resolved{HostName: "server1.example", User: "dev", Port: "22"}, nil
	case api.DaemonStatusMethod:
		return api.DaemonStatus{HelloResult: api.HelloResult{APIVersion: api.Version, DaemonVersion: "test", PID: 123}, Tunnels: []supervisor.Status{}}, nil
	case "tool.list":
		return []api.ToolInfo{{Name: "ssh", Path: "/test/ssh", Version: "OpenSSH test"}, {Name: "cloudflared", Path: "/test/cloudflared", Version: "test"}, {Name: "ngrok", Error: "not found"}}, nil
	case "config.overview":
		return api.Overview{KeychainAvailable: true, KeychainName: "Test Keychain", Tunnels: fixtureTunnels()}, nil
	default:
		return nil, api.NewError(api.Internal, "unexpected method "+method)
	}
}

func TestReadCommands(t *testing.T) {
	fakeDaemon(t, readFixture)
	for _, tc := range []struct {
		args []string
		code int
		text string
	}{
		{[]string{"ls"}, 0, "db-prod"},
		{[]string{"ls", "--json", "--running"}, 0, `"name":"Production DB"`},
		{[]string{"ls", "--type", "invalid"}, 2, "unknown tunnel type"},
		{[]string{"show", "db-p"}, 0, "Command: ssh -N"},
		{[]string{"show", "db-p", "--json"}, 0, `"findings"`},
		{[]string{"addr", "Production DB"}, 0, "127.0.0.1:15432\n"},
		{[]string{"addr", "db-prod", "--port"}, 0, "15432\n"},
		{[]string{"addr", "db-prod", "--all"}, 0, "127.0.0.1:15432\n"},
		{[]string{"addr", "db-prod", "--all", "--port"}, 2, "cannot be used together"},
		{[]string{"addr", "web"}, 1, "no endpoint"},
		{[]string{"show", "missing"}, 2, "unknown tunnel"},
		{[]string{"show", "db"}, 2, "db-prod, db-test"},
		{[]string{"logs", "db-prod", "-n", "2"}, 0, "second\nthird\n"},
		{[]string{"logs", "db-prod", "-n", "-1"}, 2, "must not be negative"},
		{[]string{"logs", "db-prod", "--since", "1m"}, 2, "no timestamps"},
		{[]string{"check", "db-prod"}, 0, "warning: test warning"},
		{[]string{"check", "db-prod", "--strict"}, 1, "checks failed"},
		{[]string{"hosts"}, 0, "server1"},
		{[]string{"hosts", "--resolve", "server1"}, 0, "server1.example"},
		{[]string{"hosts", "--json"}, 0, `"warnings":["test SSH warning"]`},
		{[]string{"doctor"}, 0, "daemon test (API 2), pid 123"},
		{[]string{"doctor", "--json"}, 0, `"available":true`},
		{[]string{"version"}, 0, "daemon test (API 2), pid 123"},
		{[]string{"daemon", "status"}, 0, "daemon test (API 2), pid 123"},
		{[]string{"daemon", "status", "--json"}, 0, `"apiVersion":2`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			args := append([]string{"--no-daemon-start"}, tc.args...)
			code := Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostics}, Options{Version: "test"})
			if code != tc.code || !strings.Contains(out.String()+diagnostics.String(), tc.text) {
				t.Fatalf("code=%d, stdout=%s, stderr=%s", code, &out, &diagnostics)
			}
			if strings.Contains(out.String()+diagnostics.String(), "\x1b[") {
				t.Fatal("non-TTY output contains color")
			}
		})
	}
}

func TestGlobalUsageAndUnreachableErrors(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	for _, tc := range []struct {
		args []string
		code int
		kind string
	}{
		{[]string{"--json", "ls", "unexpected"}, 2, "usage"},
		{[]string{"--json", "ls", "--bad"}, 2, "usage"},
		{[]string{"ls", "--bad", "--json"}, 2, "usage"},
		{[]string{"--json", "unknown"}, 2, "usage"},
		{[]string{"--json", "ls", "--timeout", "0"}, 2, "usage"},
		{[]string{"--json", "ls", "--no-daemon-start"}, 7, "daemon"},
		{[]string{"--json", "daemon", "status"}, 7, "daemon"},
		{[]string{"--json", "version"}, 0, ""},
	} {
		var out, diagnostics bytes.Buffer
		if code := Run(context.Background(), tc.args, IO{Out: &out, Err: &diagnostics}, Options{}); code != tc.code {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", tc.args, code, &out, &diagnostics)
		}
		if tc.code == 0 {
			continue
		}
		var envelope struct {
			APIVersion int `json:"apiVersion"`
			Error      struct{ Code string }
		}
		if err := json.Unmarshal(diagnostics.Bytes(), &envelope); err != nil || envelope.APIVersion != api.Version || envelope.Error.Code != tc.kind || out.Len() != 0 {
			t.Errorf("%v: envelope=%+v err=%v stdout=%s stderr=%s", tc.args, envelope, err, &out, &diagnostics)
		}
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method == "tunnel.list" {
			<-release
		}
		return readFixture(method, params)
	})
	var out, diagnostics bytes.Buffer
	code := Run(context.Background(), []string{"ls", "--json", "--no-daemon-start", "--timeout", "20ms"}, IO{Out: &out, Err: &diagnostics}, Options{Version: "test"})
	if code != 6 || out.Len() != 0 || !strings.Contains(diagnostics.String(), `"code":"timeout"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	}
}

func TestLogsFollowAndCancel(t *testing.T) {
	var polls atomic.Int32
	pollStarted, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method != "logs.since" {
			return readFixture(method, params)
		}
		polls.Add(1)
		var p api.LogsParams
		if err := json.Unmarshal(params, &p); err != nil {
			t.Error(err)
		}
		if p.Sequence == 0 {
			return api.LogsResult{Sequence: 2, Lines: []supervisor.LogLine{{Seq: 1, Line: "old"}, {Seq: 2, Line: "initial"}}}, nil
		}
		if p.Sequence == 2 {
			return api.LogsResult{Sequence: 3, Lines: []supervisor.LogLine{{Seq: 3, Line: "followed"}}}, nil
		}
		// Both lines have been written by the time the next poll arrives.
		// Cancel with this RPC in flight, instead of racing a fixed deadline
		// against the poller's 250ms ticker.
		close(pollStarted)
		<-release
		return api.LogsResult{Sequence: 3}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, diagnostics bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"logs", "db-prod", "-f", "-n", "1", "--no-daemon-start"}, IO{Out: &out, Err: &diagnostics}, Options{Version: "test"})
	}()
	watchdog := time.NewTimer(5 * time.Second)
	defer watchdog.Stop()
	select {
	case <-pollStarted:
		cancel()
	case code := <-done:
		t.Fatalf("follow exited before cancellation: code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	case <-watchdog.C:
		t.Fatal("follow did not reach the in-flight poll")
	}
	select {
	case code := <-done:
		if code != 0 || out.String() != "initial\nfollowed\n" || diagnostics.Len() != 0 || polls.Load() != 3 {
			t.Fatalf("code=%d stdout=%s stderr=%s polls=%d", code, &out, &diagnostics, polls.Load())
		}
	case <-watchdog.C:
		t.Fatal("follow did not exit after cancellation")
	}
}

func TestLogsFollowRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method != "logs.since" {
			return readFixture(method, params)
		}
		var p api.LogsParams
		if err := json.Unmarshal(params, &p); err != nil {
			t.Error(err)
		}
		if p.Sequence == 0 {
			return api.LogsResult{Sequence: 1, Lines: []supervisor.LogLine{{Seq: 1, Line: "initial"}}}, nil
		}
		<-release
		return api.LogsResult{Sequence: 1}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	var a *app
	a = newApp(IO{Out: callbackOutput(func(p []byte) (int, error) {
		// Finish setup and initial output before imposing a short timeout on
		// follow requests. The parent's deadline remains far in the future.
		a.timeout = 10 * time.Millisecond
		return out.Write(p)
	})}, Options{})
	a.noDaemonStart = true
	defer a.close()
	err := a.readLogs(ctx, "db-prod", 1, true)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || out.String() != "initial\n" {
		t.Fatalf("request timeout must not become a clean follow cancellation: err=%v parent=%v stdout=%s", err, ctx.Err(), &out)
	}
}

func TestDoctorEssentialFailuresAndQuietHosts(t *testing.T) {
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method == "config.overview" {
			return api.Overview{KeychainAvailable: false}, nil
		}
		return readFixture(method, params)
	})
	var out, diagnostics bytes.Buffer
	if code := Run(context.Background(), []string{"doctor", "--no-daemon-start"}, IO{Out: &out, Err: &diagnostics}, Options{Version: "test"}); code != 1 || !strings.Contains(out.String(), "keychain is unavailable") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	}
	diagnostics.Reset()
	if code := Run(context.Background(), []string{"hosts", "--quiet", "--no-daemon-start"}, IO{Out: io.Discard, Err: &diagnostics}, Options{Version: "test"}); code != 0 || diagnostics.Len() != 0 {
		t.Fatalf("quiet hosts code=%d stderr=%s", code, &diagnostics)
	}
}

func TestApprovalOutputLabelsAgentProvidedName(t *testing.T) {
	var out bytes.Buffer
	request := api.Approval{ID: "ap_test", Summary: "Daemon-authored summary", RequestedName: "Agent <name>\nforged", Purpose: "purpose\nnot instructions", Actor: api.Actor{Kind: "mcp", Name: "Claude Desktop"}, ExpiresAt: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	if err := writeApproval(&out, request, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{`requested name (agent-provided): "Agent <name>\nforged"`, `purpose (agent-provided): "purpose\nnot instructions"`, "Daemon-authored summary"} {
		if !strings.Contains(text, want) {
			t.Fatalf("approval output missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "\nforged") || strings.Contains(text, "\nnot instructions") {
		t.Fatalf("agent text forged output lines: %s", text)
	}
}
