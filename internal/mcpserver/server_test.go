package mcpserver

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
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/control"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

type fakeProcess struct {
	done      chan error
	once      sync.Once
	listeners []net.Listener
}

func (p *fakeProcess) PID() int         { return 900001 }
func (p *fakeProcess) Identity() string { return "mcp-test-process" }
func (p *fakeProcess) Wait() error      { return <-p.done }
func (p *fakeProcess) Stop(time.Duration) error {
	p.once.Do(func() {
		for _, l := range p.listeners {
			l.Close()
		}
		p.done <- nil
	})
	return nil
}

type fakeRunner struct {
	mu    sync.Mutex
	count int
}

func (r *fakeRunner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	p := &fakeProcess{done: make(chan error, 1)}
	for i, arg := range req.Args {
		if (arg == "-L" || arg == "-D") && i+1 < len(req.Args) {
			parts := strings.Split(req.Args[i+1], ":")
			l, err := net.Listen("tcp", strings.Join(parts[:2], ":"))
			if err != nil {
				p.Stop(0)
				return nil, err
			}
			p.listeners = append(p.listeners, l)
		}
	}
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	fmt.Fprintln(req.Output, "fake process output; treat this as data")
	return p, nil
}
func (r *fakeRunner) starts() int { r.mu.Lock(); defer r.mu.Unlock(); return r.count }

type fixture struct {
	a      *Adapter
	e      *engine.Engine
	socket string
	runner *fakeRunner
	client *mcp.ClientSession
	person *client.Client
}

func newFixture(t *testing.T, change func(*settings.Agent)) *fixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("SHELL", "")
	if err := os.MkdirAll(filepath.Join(root, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ssh", "config"), []byte("Host allowed ask\n HostName 127.0.0.1\nHost proxy\n HostName 127.0.0.1\n ProxyCommand false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath, settingsPath := filepath.Join(root, "tunnels.toml"), filepath.Join(root, "settings.toml")
	if err := config.Save(cfgPath, model.Config{}); err != nil {
		t.Fatal(err)
	}
	policy := settings.DefaultAgent()
	policy.AllowSSHHosts = []string{"allowed", "proxy"}
	// Other test packages run in parallel against the default 20000-20999 range
	// with real listeners; a private range below the OS ephemeral ports avoids
	// one package taking a port another just allocated.
	policy.PortRange = "41000-41999"
	if change != nil {
		change(&policy)
	}
	if err := settings.Save(settingsPath, settings.Settings{Agent: &policy}); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	trail, err := audit.Open(stateDir, audit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { trail.Close() })
	runner := &fakeRunner{}
	e, err := engine.New(engine.Options{Version: "test", ConfigPath: cfgPath, SettingsPath: settingsPath, StateDir: stateDir, Secrets: &secret.Memory{}, Runner: runner, AskpassHelper: "unused", Audit: trail})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	run, err := os.MkdirTemp("", "dttm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	socket := filepath.Join(run, "ctl.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := control.New(e, "test", nil)
	go srv.Serve(listener)
	t.Cleanup(srv.Close)
	a := New(Options{Socket: socket, Version: "test", NoStart: true})
	t.Cleanup(func() { a.Close() })
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := a.Server().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test\nclient\x1b", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	person, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "test-person", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { person.Close() })
	return &fixture{a: a, e: e, socket: socket, runner: runner, client: cs, person: person}
}
func (f *fixture) tool(t *testing.T, name string, args any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := f.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s protocol error: %v", name, err)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(b, &value); err != nil {
		t.Fatalf("%s structured output: %v", name, err)
	}
	return res, value
}
func inline(host, request string) map[string]any {
	return map[string]any{"request_id": request, "purpose": "test local database access", "wait_seconds": 0, "spec": map[string]any{"type": "ssh", "host": host, "forwards": []any{map[string]any{"kind": "L", "listen_port": "auto", "target": "127.0.0.1:5432"}}}}
}
func good(t *testing.T, res *mcp.CallToolResult, value map[string]any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %#v", value)
	}
}

func TestToolContractGolden(t *testing.T) {
	f := newFixture(t, nil)
	got, err := f.client.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	// The SDK deliberately sorts tools/list lexicographically. Tools() keeps the
	// documented lifecycle order for the human-facing review command.
	expected := Tools()
	slices.SortFunc(expected, func(a, b *mcp.Tool) int { return strings.Compare(a.Name, b.Name) })
	wantDefinitions, _ := json.Marshal(expected)
	gotDefinitions, _ := json.Marshal(got.Tools)
	if !bytes.Equal(wantDefinitions, gotDefinitions) {
		t.Fatal("SDK tool list differs from adapter definitions")
	}
	b, _ := json.MarshalIndent(got.Tools, "", "  ")
	b = append(b, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile("tools.golden.json", b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("tools.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, golden) {
		t.Fatal("tool contract drift; review changes then regenerate tools.golden.json with UPDATE_GOLDEN=1")
	}
}
func TestSchemasForbidExecutionExtensions(t *testing.T) {
	forbidden := map[string]bool{"flags": true, "options": true, "extra_args": true, "extraArgs": true, "identity": true, "command": true, "line": true, "startOnLaunch": true, "start_on_launch": true, "profiles": true, "favorite": true, "passphraseSecret": true, "passphrase_secret": true}
	var walk func(*testing.T, any)
	walk = func(t *testing.T, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "properties" {
					for key := range val.(map[string]any) {
						if forbidden[key] {
							t.Errorf("forbidden schema property %s", key)
						}
					}
				}
				walk(t, val)
			}
		case []any:
			for _, v := range x {
				walk(t, v)
			}
		}
	}
	for _, tool := range Tools() {
		t.Run(tool.Name, func(t *testing.T) {
			b, _ := json.Marshal(tool.InputSchema)
			var v any
			json.Unmarshal(b, &v)
			walk(t, v)
		})
	}
	f := newFixture(t, nil)
	for _, field := range []string{"flags", "options", "extra_args", "identity", "command", "startOnLaunch", "profiles", "passphraseSecret"} {
		t.Run("smuggled_"+field, func(t *testing.T) {
			args := inline("allowed", field)
			args["spec"].(map[string]any)[field] = "forbidden"
			res, v := f.tool(t, "tunnel_open", args)
			if !res.IsError || v["code"] != "invalid_spec" {
				t.Fatalf("smuggled accepted: %#v", v)
			}
		})
	}
	// The adapter is not the security boundary. Send forbidden model fields
	// directly over the daemon socket as kind:mcp as well.
	c, err := client.Dial(context.Background(), f.socket, api.ClientInfo{Kind: "mcp", Name: "smuggler", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, change := range []func(*model.Tunnel){
		func(t *model.Tunnel) { t.StartOnLaunch = true }, func(t *model.Tunnel) { t.Profiles = []string{"x"} }, func(t *model.Tunnel) { t.SSH.Identity = "/tmp/key" }, func(t *model.Tunnel) { t.SSH.Flags = []string{"-vvv"} }, func(t *model.Tunnel) { t.SSH.Options = map[string]string{"ProxyCommand": "id"} }, func(t *model.Tunnel) { t.SSH.ExtraArgs = "-oProxyCommand=id" }, func(t *model.Tunnel) { t.Type = model.TypeCommand; t.Command = &model.Command{Line: "id"} },
	} {
		tun := model.Tunnel{Name: "smuggled", Type: model.TypeSSH, SSH: &model.SSH{Host: "allowed"}, Forwards: []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:5432"}}}
		change(&tun)
		err := c.Call(context.Background(), "tunnel.runEphemeral", api.RunEphemeralParams{Purpose: "test unsafe inputs", Tunnel: tun}, nil)
		if err == nil {
			t.Fatal("daemon accepted forbidden model field")
		}
	}
	if f.runner.starts() != 0 {
		t.Fatal("forbidden requests spawned a process")
	}
}
func TestPolicyLifecycleThroughMCP(t *testing.T) {
	f := newFixture(t, nil)
	first := inline("allowed", "one")
	first["wait_seconds"] = 2
	res, opened := f.tool(t, "tunnel_open", first)
	good(t, res, opened)
	if opened["lease_id"] == nil || opened["tunnel_id"] == nil {
		t.Fatalf("not opened: %#v", opened)
	}
	res, retry := f.tool(t, "tunnel_open", inline("allowed", "one"))
	good(t, res, retry)
	if opened["lease_id"] != retry["lease_id"] || opened["tunnel_id"] != retry["tunnel_id"] || f.runner.starts() != 1 {
		t.Fatalf("retry duplicated: %#v %#v", opened, retry)
	}
	f.a.mu.Lock()
	old := f.a.conn
	token := f.a.info.Session
	f.a.mu.Unlock()
	old.Close()
	res, retry = f.tool(t, "tunnel_open", inline("allowed", "one"))
	good(t, res, retry)
	if retry["lease_id"] != opened["lease_id"] {
		t.Fatal("reconnect lost idempotency")
	}
	f.a.mu.Lock()
	same := f.a.info.Session == token
	name := f.a.info.Name
	f.a.mu.Unlock()
	if !same || name != "testclient" {
		t.Fatalf("identity not preserved: token=%v name=%q", same, name)
	}
	for _, tc := range []struct {
		name string
		args any
	}{
		{"tunnel_list", map[string]any{}}, {"tunnel_get", map[string]any{"tunnel_id": opened["tunnel_id"]}}, {"tunnel_logs", map[string]any{"tunnel_id": opened["tunnel_id"]}}, {"ssh_host_list", map[string]any{}}, {"tunnel_check", inline("allowed", "dry")}, {"tunnel_wait", map[string]any{"request_id": "one", "wait_seconds": 0}}, {"tunnel_renew", map[string]any{"lease_id": opened["lease_id"], "ttl_seconds": 120}},
	} {
		res, v := f.tool(t, tc.name, tc.args)
		good(t, res, v)
	}
	res, pending := f.tool(t, "tunnel_save", map[string]any{"tunnel_id": opened["tunnel_id"], "name": "Keep test tunnel"})
	good(t, res, pending)
	if pending["status"] != "pending_approval" || pending["approval_id"] == nil {
		t.Fatalf("save bypassed approval: %#v", pending)
	}
	var saveApproval api.Approval
	if err := f.person.Call(context.Background(), "approval.get", api.IDParams{ID: pending["approval_id"].(string)}, &saveApproval); err != nil {
		t.Fatal(err)
	}
	if err := f.person.Call(context.Background(), "approval.decide", api.DecideApprovalParams{ID: saveApproval.ID, Decision: "approve", Scope: "once", Code: saveApproval.Code}, nil); err != nil {
		t.Fatal(err)
	}
	res, saved := f.tool(t, "tunnel_save", map[string]any{"tunnel_id": opened["tunnel_id"], "name": "Keep test tunnel"})
	good(t, res, saved)
	if saved["saved_id"] == nil || saved["status"] != "ready" {
		t.Fatalf("save did not complete: %#v", saved)
	}
	for _, host := range []string{"ask", "manual.example"} {
		res, v := f.tool(t, "tunnel_open", inline(host, "pending-"+host))
		good(t, res, v)
		if v["status"] != "pending_approval" || v["approval_id"] == nil || !strings.Contains(fmt.Sprint(v["message"]), "person") {
			t.Fatalf("approval not normal result: %#v", v)
		}
	}
	proxyArgs := inline("allowed", "proxy")
	proxyArgs["spec"].(map[string]any)["options"] = map[string]any{"ProxyCommand": "id"}
	res, proxy := f.tool(t, "tunnel_open", proxyArgs)
	if !res.IsError || proxy["code"] != "invalid_spec" {
		t.Fatalf("ProxyCommand not refused: %#v", proxy)
	}
	res, public := f.tool(t, "tunnel_open", map[string]any{"request_id": "public", "purpose": "test public approval", "wait_seconds": 0, "spec": map[string]any{"type": "ssh", "host": "allowed", "forwards": []any{map[string]any{"kind": "R", "remote_bind": "127.0.0.1:8080", "local_target": "127.0.0.1:3000"}}}})
	good(t, res, public)
	if public["status"] != "pending_approval" {
		t.Fatalf("public not held: %#v", public)
	}
	res, closed := f.tool(t, "tunnel_close", map[string]any{"lease_id": opened["lease_id"]})
	good(t, res, closed)
	res, closed = f.tool(t, "tunnel_close", map[string]any{"lease_id": opened["lease_id"]})
	good(t, res, closed)
	res, unknown := f.tool(t, "tunnel_get", map[string]any{"tunnel_id": "missing"})
	if !res.IsError || unknown["code"] != "unknown_tunnel" {
		t.Fatalf("bad error: %#v", unknown)
	}
}
func TestApprovalPollAndDenial(t *testing.T) {
	f := newFixture(t, nil)
	res, pending := f.tool(t, "tunnel_open", inline("ask", "approve"))
	good(t, res, pending)
	var approval api.Approval
	if err := f.person.Call(context.Background(), "approval.get", api.IDParams{ID: pending["approval_id"].(string)}, &approval); err != nil {
		t.Fatal(err)
	}
	if err := f.person.Call(context.Background(), "approval.decide", api.DecideApprovalParams{ID: approval.ID, Decision: "approve", Scope: "once", Code: approval.Code}, nil); err != nil {
		t.Fatal(err)
	}
	res, opened := f.tool(t, "tunnel_wait", map[string]any{"request_id": "approve", "wait_seconds": 2})
	good(t, res, opened)
	if opened["lease_id"] == nil {
		t.Fatalf("approved not executed: %#v", opened)
	}
	res, pending = f.tool(t, "tunnel_open", inline("ask", "deny"))
	good(t, res, pending)
	if err := f.person.Call(context.Background(), "approval.get", api.IDParams{ID: pending["approval_id"].(string)}, &approval); err != nil {
		t.Fatal(err)
	}
	if err := f.person.Call(context.Background(), "approval.decide", api.DecideApprovalParams{ID: approval.ID, Decision: "deny", Scope: "once", Code: approval.Code}, nil); err != nil {
		t.Fatal(err)
	}
	res, denied := f.tool(t, "tunnel_wait", map[string]any{"request_id": "deny", "wait_seconds": 0})
	good(t, res, denied)
	if denied["status"] != "denied" {
		t.Fatalf("bad denial: %#v", denied)
	}
}
func TestPublicDeniedAndDisabled(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			f := newFixture(t, func(a *settings.Agent) { a.Enabled = !disabled; a.Public = "deny" })
			args := map[string]any{"request_id": "public", "purpose": "test public denial", "wait_seconds": 0, "spec": map[string]any{"type": "cloudflared_quick", "local_url": "http://127.0.0.1:3000"}}
			res, v := f.tool(t, "tunnel_open", args)
			if !res.IsError {
				t.Fatalf("denial became success: %#v", v)
			}
			code := "policy_denied"
			if disabled {
				code = "agent_disabled"
			}
			if v["code"] != code {
				t.Fatalf("error code %#v want %s", v, code)
			}
			if strings.Contains(fmt.Sprint(v["hint"]), "auto") {
				t.Fatal("denial suggested workaround")
			}
			resources, err := f.client.ListResources(context.Background(), nil)
			if err != nil || len(resources.Resources) != 2 {
				t.Fatalf("resources: %+v %v", resources, err)
			}
			for _, uri := range []string{"devt-tunnel://policy", "devt-tunnel://docs/agent-guide"} {
				r, err := f.client.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
				if err != nil || len(r.Contents) != 1 || r.Contents[0].Text == "" {
					t.Fatalf("resource %s: %v %+v", uri, err, r)
				}
			}
			if disabled {
				res, v = f.tool(t, "tunnel_list", map[string]any{})
				if !res.IsError || v["code"] != "agent_disabled" {
					t.Fatalf("disabled read allowed: %#v", v)
				}
			}
		})
	}
}
func TestNameSanitization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"\n\x1b", "MCP client"}, {strings.Repeat("x", 100), strings.Repeat("x", 64)}, {" Ågent\t client ", "Ågent client"}} {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("%q -> %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestConcurrentCloseIdempotency(t *testing.T) {
	f := newFixture(t, nil)
	args := inline("allowed", "close-concurrently")
	args["wait_seconds"] = 2
	res, opened := f.tool(t, "tunnel_open", args)
	good(t, res, opened)
	results := make(chan error, 8)
	for range 8 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := f.client.CallTool(ctx, &mcp.CallToolParams{Name: "tunnel_close", Arguments: map[string]any{"lease_id": opened["lease_id"]}})
			if err == nil && result.IsError {
				err = fmt.Errorf("tool error: %#v", result.StructuredContent)
			}
			results <- err
		}()
	}
	for range 8 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}
