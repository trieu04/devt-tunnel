package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func testServer(t *testing.T) (*Server, string) { return testServerWithOptions(t, nil) }

func testServerWithOptions(t *testing.T, prepare func(*engine.Options)) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("SHELL", "")
	// Short socket paths keep tests independent of testing.T's long directory names.
	run, err := os.MkdirTemp("", "dttc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	socket := filepath.Join(run, "ctl.sock")
	t.Setenv("DTT_SOCKET", socket)
	path := filepath.Join(root, "tunnels.toml")
	err = config.Save(path, model.Config{Tunnels: []model.Tunnel{{ID: "t", Name: "Test", Type: model.TypeCommand, Command: &model.Command{Line: "echo ready", ReadyLog: "ready"}}}})
	if err != nil {
		t.Fatal(err)
	}
	opts := engine.Options{ConfigPath: path, SettingsPath: filepath.Join(root, "settings.toml"), StateDir: filepath.Join(root, "state"), Secrets: &secret.Memory{}, AskpassHelper: "unused"}
	if prepare != nil {
		prepare(&opts)
	}
	if opts.Audit == nil {
		trail, err := audit.Open(opts.StateDir, audit.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { trail.Close() })
		opts.Audit = trail
	}
	e, err := engine.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	s := New(e, "test", nil)
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(l)
	t.Cleanup(s.Close)
	return s, socket
}

func TestMethodContract(t *testing.T) {
	s, socket := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "contract", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	seen := map[string]bool{api.DaemonHello: true}
	cases := []struct {
		method string
		params any
	}{
		{api.DaemonStatusMethod, api.Empty{}}, {api.EventsSubscribe, api.Empty{}},
		{"policy.get", api.Empty{}},
		{"tunnel.openRequest", api.OpenRequestParams{RequestID: "contract-request", ID: "absent", Purpose: "contract"}},
		{"tunnel.waitRequest", api.WaitRequestParams{RequestID: "contract-request"}},
		{"tunnel.checkRequest", api.OpenRequestParams{ID: "t"}},
		{"approval.list", api.Empty{}}, {"approval.get", api.IDParams{ID: "absent"}},
		{"approval.decide", api.DecideApprovalParams{ID: "absent", Decision: "deny", Scope: "once"}},
		{"audit.read", api.AuditReadParams{}},
		{"tunnel.promote", api.PromoteTunnelParams{ID: "absent"}},
		{"tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: model.Tunnel{ID: "invalid"}}},
		{"lease.open", api.OpenLeaseParams{ID: "absent"}}, {"lease.renew", api.RenewLeaseParams{ID: "absent"}},
		{"lease.close", api.IDParams{ID: "absent"}}, {"lease.list", api.Empty{}}, {"lease.revoke", api.IDParams{ID: "absent"}},
		{"tunnel.list", api.Empty{}}, {"tunnel.get", api.IDParams{ID: "t"}},
		{"tunnel.save", api.SaveTunnelParams{Tunnel: model.Tunnel{Name: "New", Type: model.TypeCommand, Command: &model.Command{Line: "echo ready", ReadyLog: "ready"}}}},
		{"tunnel.delete", api.IDParams{ID: "new"}}, {"tunnel.start", api.IDParams{ID: "absent"}},
		{"tunnel.stop", api.IDParams{ID: "absent"}}, {"tunnel.restart", api.IDParams{ID: "absent"}}, {"tunnel.waitReady", api.IDParams{ID: "t"}},
		{"tunnel.evaluateDraft", api.TunnelParams{Tunnel: model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: "echo test"}}}},
		{"tunnel.import", api.ImportParams{Line: "ssh -N -L 1234:127.0.0.1:5432 test"}}, {"tunnel.newDraft", api.NewDraftParams{Kind: model.TypeSSH}},
		{"tunnel.duplicateDraft", api.IDParams{ID: "t"}}, {"tunnel.setFavorite", api.FavoriteParams{ID: "t", Favorite: true}},
		{"tunnel.findings", api.IDParams{ID: "t"}}, {"tunnel.recordUsed", api.IDParams{ID: "t"}}, {"tunnel.startOnLaunch", api.Empty{}},
		{"logs.read", api.IDParams{ID: "t"}}, {"logs.since", api.LogsParams{ID: "t"}},
		{"profile.list", api.Empty{}}, {"profile.save", api.SaveProfileParams{Profile: model.Profile{Name: "Work"}, Members: []string{"t"}}},
		{"profile.duplicate", api.IDParams{ID: "work"}}, {"profile.delete", api.IDParams{ID: "work"}},
		{"group.list", api.Empty{}}, {"group.save", api.SaveGroupParams{Group: model.Group{Name: "Stack"}, Members: []string{"t"}}},
		{"group.duplicate", api.IDParams{ID: "stack"}},
		{"group.start", api.IDParams{ID: "absent"}}, {"group.stop", api.IDParams{ID: "stack"}}, {"group.delete", api.IDParams{ID: "stack"}},
		{"secret.list", api.Empty{}}, {"secret.set", api.SetSecretParams{Name: "disposable", Value: "test-value", Kind: secret.KindEnv}},
		{"secret.rename", api.RenameSecretParams{From: "disposable", To: "renamed"}}, {"secret.delete", api.SecretNameParams{Name: "renamed"}}, {"secret.forget", api.Empty{}},
		{"prompt.list", api.Empty{}}, {"prompt.answer", api.AnswerPromptParams{ID: "absent", Cancelled: true}},
		{"ssh.hosts", api.SSHParams{}}, {"ssh.resolve", api.SSHParams{Alias: ""}}, {"ssh.reload", api.Empty{}},
		{"tool.list", api.Empty{}}, {"tool.setPath", api.ToolPathParams{Tool: "ssh", Path: ""}},
		{"settings.getAgent", api.Empty{}}, {"settings.setAgent", api.AgentSettingsParams{Agent: settings.DefaultAgent()}},
		{"settings.get", api.Empty{}}, {"settings.setShortcut", api.ShortcutParams{Shortcut: "Alt+Space"}}, {"settings.setUpdateChecks", api.UpdateChecksParams{Enabled: false}},
		{"orphan.list", api.Empty{}}, {"orphan.stop", api.OrphanParams{PID: -1}},
		{"config.overview", api.Empty{}}, {"config.path", api.Empty{}}, {"config.reload", api.Empty{}}, {"system.wake", api.Empty{}}, {"system.focus", api.Empty{}},
		{api.DaemonShutdown, api.ShutdownParams{}},
	}
	for _, tc := range cases {
		err := c.Call(ctx, tc.method, tc.params, nil)
		var rpc *api.Error
		if err != nil && (!errors.As(err, &rpc) || rpc.Code == -32601 || rpc.Code == -32602) {
			t.Errorf("%s: %v", tc.method, err)
		}
		if err != nil && tc.method != "tunnel.openRequest" && tc.method != "tunnel.waitRequest" && tc.method != "approval.get" && tc.method != "approval.decide" && tc.method != "tunnel.promote" && tc.method != "tunnel.runEphemeral" && tc.method != "lease.open" && tc.method != "lease.renew" && tc.method != "lease.close" && tc.method != "lease.revoke" && tc.method != "tunnel.start" && tc.method != "tunnel.stop" && tc.method != "tunnel.restart" && tc.method != "tunnel.waitReady" && tc.method != "ssh.resolve" && tc.method != "orphan.stop" {
			t.Errorf("%s unexpectedly failed: %v", tc.method, err)
		}
		seen[tc.method] = true
	}
	for _, method := range api.Methods {
		if !seen[method] || s.handlers[method] == nil {
			t.Errorf("uncovered method %s", method)
		}
	}
	for method := range s.handlers {
		if !seen[method] {
			t.Errorf("handler missing from contract %s", method)
		}
	}
}

func TestHelloFirstAndVersionGuard(t *testing.T) {
	_, socket := testServer(t)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
	enc.Encode(api.Request{JSONRPC: "2.0", ID: json.RawMessage(`"first"`), Method: "tunnel.list"})
	var response api.Response
	if err = dec.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Data.Kind != api.Incompatible {
		t.Fatalf("no hello accepted: %+v", response)
	}
	conn.Close()
	conn, err = net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	enc, dec = json.NewEncoder(conn), json.NewDecoder(conn)
	params, _ := json.Marshal(api.HelloParams{APIVersion: 99, Client: api.ClientInfo{Kind: "cli", Name: "future"}})
	enc.Encode(api.Request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: api.DaemonHello, Params: params})
	if err = dec.Decode(&response); err != nil {
		t.Fatal(err)
	}
	enc.Encode(api.Request{JSONRPC: "2.0", ID: json.RawMessage("2"), Method: "tunnel.list"})
	if err = dec.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Data.Kind != api.Incompatible {
		t.Fatal("incompatible client could list")
	}
	enc.Encode(api.Request{JSONRPC: "2.0", ID: json.RawMessage("3"), Method: api.DaemonStatusMethod})
	response = api.Response{}
	if err = dec.Decode(&response); err != nil || response.Error != nil {
		t.Fatalf("recovery status: %v %+v", err, response)
	}
}

func TestWireErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind api.ErrorKind
	}{{config.ErrConflict, api.Conflict}, {supervisor.ErrNeedsSecret, api.NeedsSecret}, {secret.ErrUnavailable, api.KeyringUnavailable}, {supervisor.ErrStopped, api.Stopped}, {context.DeadlineExceeded, api.Timeout}} {
		if got := wireError(tc.err); got.Data.Kind != tc.kind {
			t.Errorf("%v: %s", tc.err, got.Data.Kind)
		}
	}
}

func TestShutdownRejectsDifferentDaemonPID(t *testing.T) {
	s, socket := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "gui", Name: "identity-test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{ExpectedPID: s.Hello().PID + 1}, nil)
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.Conflict {
		t.Fatalf("shutdown error = %v, want identity conflict", err)
	}
	if s.stopping.Load() {
		t.Fatal("mismatched shutdown stopped daemon")
	}
	if err = c.Call(ctx, api.DaemonStatusMethod, api.Empty{}, nil); err != nil {
		t.Fatalf("daemon unavailable after rejected shutdown: %v", err)
	}
}

func TestConcurrentWaitEventsAndDisconnect(t *testing.T) {
	s, socket := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "concurrent", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	events := make(chan api.Event, 8)
	c.Events(func(e api.Event) {
		select {
		case events <- e:
		default:
		}
	})
	if err = c.Call(ctx, api.EventsSubscribe, api.Empty{}, nil); err != nil {
		t.Fatal(err)
	}
	line := "sleep 30"
	if runtime.GOOS == "windows" {
		line = "ping -n 30 127.0.0.1 > nul"
	}
	var id string
	if err = c.Call(ctx, "tunnel.save", api.SaveTunnelParams{Tunnel: model.Tunnel{Name: "Pending", Type: model.TypeCommand, Command: &model.Command{Line: line, ReadyLog: "never"}}}, &id); err != nil {
		t.Fatal(err)
	}
	if err = c.Call(ctx, "tunnel.start", api.IDParams{ID: id}, nil); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: id}, nil) }()
	if err = c.Call(ctx, api.DaemonStatusMethod, api.Empty{}, nil); err != nil {
		t.Fatal("wait blocked status:", err)
	}
	if err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{OnlyIfIdle: true}, nil); err == nil {
		t.Fatal("idle upgrade stopped active tunnel")
	}
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("no events")
	}
	c.Close()
	select {
	case <-waited:
	case <-ctx.Done():
		t.Fatal("disconnect did not cancel call")
	}
	s.engine.Stop(id)
}

func TestSlowEventConsumerDisconnects(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &connection{conn: server, ctx: ctx, cancel: cancel, events: make(chan api.Event, 1)}
	c.subscribed.Store(true)
	c.enqueue(api.Event{Type: api.EventTunnelsChanged})
	c.enqueue(api.Event{Type: api.EventTunnelsChanged})
	select {
	case <-ctx.Done():
	default:
		t.Fatal("slow subscriber retained connection")
	}
}

func TestMCPPromptAnswerIsRefused(t *testing.T) {
	_, socket := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "mcp", Name: "agent", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Call(ctx, "prompt.answer", api.AnswerPromptParams{ID: "absent", Value: "yes"}, nil); err == nil {
		t.Fatal("MCP prompt answer accepted")
	}
}

func TestWriterDeadline(t *testing.T) {
	server, peer := net.Pipe()
	defer peer.Close()
	defer server.Close()
	c := &connection{conn: server}
	started := time.Now()
	if err := c.write(api.Empty{}); err == nil {
		t.Fatal("blocked writer succeeded")
	}
	if time.Since(started) > 7*time.Second {
		t.Fatal("writer deadline not bounded")
	}
}
