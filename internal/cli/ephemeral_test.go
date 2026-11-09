package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

// Keep the actual engine, RPC server, allocation and readiness machinery. Only
// the OS process is replaced; its listeners are real loopback sockets.
type ephemeralRunner struct{}
type ephemeralProcess struct {
	exit      chan error
	once      sync.Once
	listeners []net.Listener
}

func (p *ephemeralProcess) PID() int         { return 12345 }
func (p *ephemeralProcess) Identity() string { return "cli-fixture-process" }
func (p *ephemeralProcess) Wait() error      { return <-p.exit }
func (p *ephemeralProcess) Stop(time.Duration) error {
	p.once.Do(func() {
		for _, l := range p.listeners {
			_ = l.Close()
		}
		p.exit <- nil
	})
	return nil
}
func (r *ephemeralRunner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	p := &ephemeralProcess{exit: make(chan error, 1)}
	for i, arg := range req.Args {
		if (arg != "-L" && arg != "-D") || i+1 >= len(req.Args) {
			continue
		}
		bind := req.Args[i+1]
		if arg == "-L" {
			parts, err := defineColonParts(bind)
			if err != nil || len(parts) != 4 {
				return nil, fmt.Errorf("fixture map %q", bind)
			}
			bind = net.JoinHostPort(parts[0], parts[1])
		}
		listener, err := net.Listen("tcp", bind)
		if err != nil {
			_ = p.Stop(0)
			return nil, err
		}
		p.listeners = append(p.listeners, listener)
		go func() {
			for {
				c, err := listener.Accept()
				if err != nil {
					return
				}
				_ = c.Close()
			}
		}()
	}
	go func() {
		_, _ = io.WriteString(req.Output, "ready\nhttps://fixture.trycloudflare.com\nRegistered tunnel connection\n")
	}()
	return p, nil
}

func ephemeralDaemon(t *testing.T) *engine.Engine {
	t.Helper()
	return defineDaemon(t, func(opts *engine.Options) { opts.Runner = &ephemeralRunner{} })
}

type ephemeralJSON struct {
	APIVersion int `json:"apiVersion"`
	Tunnels    []struct {
		ID        string          `json:"id"`
		State     string          `json:"state"`
		Owner     api.TunnelOwner `json:"owner"`
		Endpoints []api.Endpoint  `json:"endpoints"`
	} `json:"tunnels"`
}

func ephemeralResult(t *testing.T, args ...string) ephemeralJSON {
	t.Helper()
	code, out, diagnostics := defineRun(append([]string{"--json"}, args...)...)
	var result ephemeralJSON
	if err := json.Unmarshal([]byte(out), &result); code != 0 || err != nil || result.APIVersion != 2 || len(result.Tunnels) != 1 {
		t.Fatalf("%v: code=%d stdout=%s stderr=%s err=%v", args, code, out, diagnostics, err)
	}
	return result
}

func TestRunDryRunRealDaemon(t *testing.T) {
	defineDaemon(t)
	for _, tc := range []struct {
		argv    []string
		command string
	}{
		{[]string{"ssh", "-N", "-L", "15432:127.0.0.1:5432", "server1"}, "-L 127.0.0.1:15432:127.0.0.1:5432 server1"},
		{[]string{"ngrok", "http", "3000"}, "ngrok http 127.0.0.1:3000 --log=stdout --log-format=logfmt"},
		{[]string{"cloudflared", "tunnel", "--url", "http://localhost:3000"}, "cloudflared tunnel --url http://localhost:3000"},
	} {
		code, out, diagnostics := defineRun(append([]string{"run", "--dry-run", "--json", "--"}, tc.argv...)...)
		var result defineResult
		if err := json.Unmarshal([]byte(out), &result); code != 0 || diagnostics != "" || err != nil || result.APIVersion != 2 || !result.DryRun || result.Saved || result.Started || !strings.Contains(result.Preview, tc.command) || len(result.Findings) == 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s decode=%v", tc.argv, code, out, diagnostics, err)
		}
	}
	if code, out, err := defineRun("ls", "--json"); code != 0 || !strings.Contains(out, `"tunnels":[]`) {
		t.Fatalf("dry run saved tunnels: %d %s %s", code, out, err)
	}
}

func TestForwardAutoJSONAndDetachedLeaseRealDaemon(t *testing.T) {
	ephemeralDaemon(t)
	result := ephemeralResult(t, "forward", "server1", "auto:5432")
	tunnel := result.Tunnels[0]
	if !strings.HasPrefix(tunnel.ID, "tmp-") || tunnel.State != "running" || len(tunnel.Endpoints) != 1 || tunnel.Endpoints[0].Listen == "" || tunnel.Endpoints[0].Target != "127.0.0.1:5432" || tunnel.Owner.Pinned || !tunnel.Owner.Ephemeral {
		t.Fatalf("forward result: %+v", tunnel)
	}
	code, out, diagnostics := defineRun("leases", "--json")
	var listed struct {
		APIVersion int `json:"apiVersion"`
		Leases     []api.Lease
	}
	if err := json.Unmarshal([]byte(out), &listed); code != 0 || err != nil || diagnostics != "" || listed.APIVersion != 2 || len(listed.Leases) != 1 {
		t.Fatalf("leases: %d %s %s %v", code, out, diagnostics, err)
	}
	lease := listed.Leases[0]
	if !lease.Detached || !lease.StartedIt || lease.TunnelID != tunnel.ID || lease.Holder.Kind != "cli" || !lease.ExpiresAt.IsZero() || lease.GraceUntil != nil {
		t.Fatalf("lease: %+v", lease)
	}
	if code, out, diagnostics := defineRun("leases"); code != 0 || !strings.Contains(out, "DETACHED") || !strings.Contains(out, "never") || !strings.Contains(out, lease.ID) {
		t.Fatalf("table: %d %s %s", code, out, diagnostics)
	}
	if code, out, diagnostics := defineRun("leases", "revoke", lease.ID, "--json"); code != 0 || !strings.Contains(out, `"revoked":true`) || diagnostics != "" {
		t.Fatalf("revoke: %d %s %s", code, out, diagnostics)
	}
	if c, err := net.DialTimeout("tcp", tunnel.Endpoints[0].Listen, time.Second); err == nil {
		c.Close()
		t.Fatal("listener survived revocation")
	}
}

func TestForwardSaveAndSaveProfileRealDaemon(t *testing.T) {
	ephemeralDaemon(t)
	result := ephemeralResult(t, "forward", "server1", "auto:5432", "--save", "Staging DB", "--ttl", "15m")
	if result.Tunnels[0].ID != "staging-db" || result.Tunnels[0].Owner.Ephemeral || result.Tunnels[0].Owner.Pinned {
		t.Fatalf("promotion: %+v", result)
	}
	stored := defineSavedTunnel(t, "staging-db")
	if len(stored.Forwards) != 1 || stored.Forwards[0].ListenPort != "" || stored.Forwards[0].Bind != result.Tunnels[0].Endpoints[0].Listen {
		t.Fatalf("saved: %+v", stored)
	}
	result = ephemeralResult(t, "forward", "server1", "auto:5432")
	code, out, diagnostics := defineRun("save", result.Tunnels[0].ID, "--name", "Profile DB", "--profile", "Work", "--json")
	if code != 0 || !strings.Contains(out, `"id":"profile-db"`) || diagnostics != "" {
		t.Fatalf("save: %d %s %s", code, out, diagnostics)
	}
	stored = defineSavedTunnel(t, "profile-db")
	if stored.Profile != "work" {
		t.Fatalf("profile membership: %+v", stored)
	}
	if code, out, diagnostics := defineRun("stop", "profile-db", "staging-db"); code != 0 {
		t.Fatalf("stop: %d %s %s", code, out, diagnostics)
	}
}

func TestForwardWarningsAndValidation(t *testing.T) {
	ephemeralDaemon(t)
	code, out, diagnostics := defineRun("forward", "server1", "0.0.0.0:15432:127.0.0.1:5432", "--json", "--quiet")
	if code != 0 || !strings.Contains(out, `"apiVersion":2`) || !strings.Contains(diagnostics, "non-loopback") {
		t.Fatalf("warning: %d %s %s", code, out, diagnostics)
	}
	if code, _, diagnostics := defineRun("stop", "--all"); code != 0 {
		t.Fatal(diagnostics)
	}
	for _, args := range [][]string{
		{"forward", "server1"}, {"forward", "server1", "5433:5432", "--ttl", "0"},
		{"forward", "server1", "--remote", "auto:5432"}, {"forward", "server1", "auto:5432", "--ttl", "9h"},
		{"forward", "server1", "auto:5432", "--dry-run"}, {"run", "ssh", "-N"},
		{"save", "tmp-x"}, {"approvals", "approve", "x", "--code", "do-not-accept"},
	} {
		if code, _, diagnostics := defineRun(args...); code != 2 {
			t.Errorf("%v: code=%d stderr=%s", args, code, diagnostics)
		}
	}
}

type readyOutput struct {
	bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *readyOutput) Write(b []byte) (int, error) {
	n, err := w.Buffer.Write(b)
	if strings.Contains(w.Buffer.String(), "127.0.0.1:") {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}
func TestRunForegroundClosesLeaseOnInterruptRealDaemon(t *testing.T) {
	ephemeralDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &readyOutput{ready: make(chan struct{})}
	var diagnostics bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"--no-daemon-start", "run", "--", "ssh", "-N", "-L", "15433:127.0.0.1:5432", "server1"}, IO{In: strings.NewReader(""), Out: output, Err: &diagnostics}, Options{})
	}()
	select {
	case <-output.ready:
	case code := <-done:
		t.Fatalf("exited before ready: %d %s", code, &diagnostics)
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("interrupt: %d %s", code, &diagnostics)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupt hung")
	}
	if code, out, diagnostics := defineRun("leases", "--json"); code != 0 || !strings.Contains(out, `"leases":[]`) {
		t.Fatalf("lease not closed: %d %s %s", code, out, diagnostics)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:15433", time.Second); err == nil {
		c.Close()
		t.Fatal("foreground listener survived interrupt")
	}
}

func TestExposePublicWarningAndViaRealDaemon(t *testing.T) {
	ephemeralDaemon(t)
	for _, tc := range []struct {
		args    []string
		code    int
		preview string
	}{
		{[]string{"expose", "3000", "--dry-run", "--json"}, 2, ""},
		{[]string{"expose", "3000", "--via", "invalid", "--dry-run"}, 2, ""},
		{[]string{"expose", "3000", "--via", "ngrok", "--dry-run", "--quiet", "--json"}, 0, "ngrok http http://127.0.0.1:3000"},
		{[]string{"expose", "http://localhost:3000", "--via", "cloudflared", "--dry-run", "--json"}, 0, "cloudflared tunnel --url http://localhost:3000"},
		{[]string{"expose", "3000", "--via", "cloudflared", "--domain", "example.com", "--dry-run"}, 2, ""},
		{[]string{"expose", "http://token@example.com", "--via", "ngrok", "--dry-run"}, 2, ""},
	} {
		code, out, diagnostics := defineRun(tc.args...)
		if code != tc.code || !strings.Contains(diagnostics, "service will be public") || !strings.Contains(out, tc.preview) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", tc.args, code, out, diagnostics)
		}
	}
	result := ephemeralResult(t, "expose", "3000", "--via", "cloudflared", "--save", "Public Demo")
	if result.Tunnels[0].ID != "public-demo" || len(result.Tunnels[0].Endpoints) == 0 || !result.Tunnels[0].Endpoints[0].Public {
		t.Fatalf("expose: %+v", result)
	}
}

func TestApprovalsAndDetachedPolicyRealDaemon(t *testing.T) {
	e := ephemeralDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, os.Getenv("DTT_SOCKET"), api.ClientInfo{Kind: "mcp", Name: "requesting-agent", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tunnel := model.Tunnel{Name: "Request", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "http://127.0.0.1:3000"}}
	var rpc *api.Error
	err = c.Call(ctx, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel, Detached: true}, nil)
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.PolicyDenied {
		t.Fatalf("mcp detached: %v", err)
	}
	for _, verb := range [][]string{{"approvals", "deny"}, {"deny"}} {
		err := c.Call(ctx, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel, Purpose: "Run a database check"}, nil)
		if !errors.As(err, &rpc) || rpc.Data.Kind != api.PendingApproval || rpc.Data.ApprovalID == "" {
			t.Fatalf("approval request: %v", err)
		}
		id := rpc.Data.ApprovalID
		code, out, diagnostics := defineRun("approvals", "--json")
		var listed struct {
			APIVersion int `json:"apiVersion"`
			Approvals  []api.Approval
		}
		if err := json.Unmarshal([]byte(out), &listed); code != 0 || err != nil || diagnostics != "" || listed.APIVersion != 2 || len(listed.Approvals) != 1 {
			t.Fatalf("approvals: %d %s %s %v", code, out, diagnostics, err)
		}
		request := listed.Approvals[0]
		if request.Code == "" || len(request.Argv) == 0 || request.Purpose == "" || len(request.Choices) == 0 || request.ExpiresAt.IsZero() {
			t.Fatalf("incomplete person request: %+v", request)
		}
		if code, out, diagnostics := defineRun("approvals"); code != 0 || !strings.Contains(out, request.Code) || !strings.Contains(out, "argv:") || !strings.Contains(out, "requesting-agent") {
			t.Fatalf("request: %d %s %s", code, out, diagnostics)
		}
		for _, args := range [][]string{{"approvals", "approve", id}, {"approve", id, "--no-input", "--json"}} {
			if code, _, diagnostics := defineRun(args...); code != 4 || !strings.Contains(diagnostics, "TTY") {
				t.Fatalf("non-TTY approve: %d %s", code, diagnostics)
			}
		}
		pending, err := e.GetApproval(api.Actor{Kind: "cli", Name: "person"}, "", id)
		if err != nil || pending.Status != "pending" {
			t.Fatalf("approve mutated without TTY: %+v %v", pending, err)
		}
		if code, out, diagnostics := defineRun(append(verb, id, "--json")...); code != 0 || !strings.Contains(out, `"status":"denied"`) || diagnostics != "" {
			t.Fatalf("deny: %d %s %s", code, out, diagnostics)
		}
		tunnel.Ngrok.Addr += "/other"
	}
}

func TestAuditFiltersRealDaemon(t *testing.T) {
	var writer *audit.Writer
	defineDaemon(t, func(opts *engine.Options) {
		var err error
		writer, err = audit.Open(filepath.Join(os.Getenv("DEVT_TUNNEL_STATE_DIR")), audit.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = writer.Close() })
		opts.Audit = writer
	})
	for _, event := range []audit.Event{
		{Actor: audit.Actor{Kind: audit.ActorCLI, Name: "alice"}, Action: audit.TunnelStart, TunnelID: "db", Result: audit.ResultOK},
		{Actor: audit.Actor{Kind: audit.ActorMCP, Name: "bob"}, Action: audit.TunnelStart, TunnelID: "db", Result: audit.ResultOK},
		{Actor: audit.Actor{Kind: audit.ActorCLI, Name: "alice"}, Action: audit.TunnelStop, TunnelID: "web", Result: audit.ResultOK},
	} {
		if _, err := writer.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		flags []string
		count int
	}{
		{nil, 3}, {[]string{"--actor", "alice"}, 2}, {[]string{"--tunnel", "db"}, 2},
		{[]string{"--actor", "alice", "--tunnel", "db", "--since", "1h"}, 1},
		{[]string{"--since", time.Now().Add(time.Hour).Format(time.RFC3339)}, 0},
	} {
		code, out, diagnostics := defineRun(append([]string{"audit", "--json"}, tc.flags...)...)
		var result struct {
			APIVersion int `json:"apiVersion"`
			Events     []audit.Event
			Next       uint64
		}
		if err := json.Unmarshal([]byte(out), &result); code != 0 || diagnostics != "" || err != nil || result.APIVersion != 2 || len(result.Events) != tc.count || result.Next != 3 {
			t.Errorf("%v: code=%d stdout=%s stderr=%s decode=%v", tc.flags, code, out, diagnostics, err)
		}
	}
	if code, _, diagnostics := defineRun("audit", "--since", "-1h"); code != 2 {
		t.Errorf("negative since: %d %s", code, diagnostics)
	}
}

func TestRunDetachRealDaemon(t *testing.T) {
	ephemeralDaemon(t)
	result := ephemeralResult(t, "run", "--detach", "--name", "One-off SSH", "--ttl", "5m", "--", "ssh", "-N", "-L", "15434:127.0.0.1:5432", "server1")
	if result.Tunnels[0].State != "running" || !result.Tunnels[0].Owner.Ephemeral || result.Tunnels[0].Owner.Expiry == nil {
		t.Fatalf("detached run: %+v", result)
	}
	if code, out, diagnostics := defineRun("stop", result.Tunnels[0].ID, "--json"); code != 0 || !strings.Contains(out, `"state":"stopped"`) {
		t.Fatalf("detached stop: %d %s %s", code, out, diagnostics)
	}
}

type callbackOutput func([]byte) (int, error)

func (f callbackOutput) Write(b []byte) (int, error) { return f(b) }

func TestAuditFollowCursorAndPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var mu sync.Mutex
	var after []uint64
	fakeDaemon(t, func(method string, raw json.RawMessage) (any, *api.Error) {
		if method != "audit.read" {
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
		var params api.AuditReadParams
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Error(err)
		}
		if params.Actor != "alice" || params.Tunnel != "db" || params.Since.IsZero() {
			t.Errorf("filters lost: %+v", params)
		}
		mu.Lock()
		after = append(after, params.After)
		n := len(after)
		mu.Unlock()
		switch n {
		case 1:
			return api.AuditReadResult{Events: []audit.Event{{Sequence: 12}}, Next: 12, Oldest: 12}, nil
		case 2:
			return api.AuditReadResult{Next: 15, Oldest: 12}, nil // Cursor advances over filtered records.
		default:
			return api.AuditReadResult{Events: []audit.Event{{Sequence: 16}}, Next: 16, Oldest: 12}, nil
		}
	})
	var out, diagnostics bytes.Buffer
	writer := callbackOutput(func(b []byte) (int, error) {
		n, err := out.Write(b)
		if bytes.Contains(b, []byte(`"seq":16`)) {
			cancel()
		}
		return n, err
	})
	code := Run(ctx, []string{"--no-daemon-start", "audit", "--actor", "alice", "--tunnel", "db", "--since", "1h", "-f", "--json"}, IO{Out: writer, Err: &diagnostics}, Options{})
	if code != 0 || diagnostics.Len() != 0 {
		t.Fatalf("follow: code=%d out=%s stderr=%s", code, &out, &diagnostics)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("follow batches: %s", &out)
	}
	for _, line := range lines {
		if !strings.Contains(line, `"apiVersion":2`) {
			t.Errorf("unversioned batch: %s", line)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(after) < 3 || after[0] != 0 || after[1] != 12 || after[2] != 15 {
		t.Fatalf("cursors: %v", after)
	}
}
