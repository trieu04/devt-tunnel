package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/control"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/mcpserver"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type mcpClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mcpClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *mcpClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (c *mcpClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type mcpSSHFixture struct {
	env     sshEnv
	engine  *engine.Engine
	clock   *mcpClock
	socket  string
	state   string
	sshUsed string
}

func newMCPSSHFixture(t *testing.T) mcpSSHFixture {
	t.Helper()
	env := setup(t)
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "SHELL": ""} {
		t.Setenv(key, value)
	}
	sshDir := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Only decrypt a disposable COPY. MCP receives an alias, never a key path
	// or passphrase, and no user SSH files or credential stores are consulted.
	key := filepath.Join(sshDir, "identity")
	data, err := os.ReadFile(env.key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("ssh-keygen", "-p", "-P", passphrase, "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("decrypt fixture key copy: %v: %s", err, output)
	}
	sshConfig := filepath.Join(sshDir, "config")
	contents := fmt.Sprintf("Host mcp-test\n  HostName 127.0.0.1\n  Port %d\n  User %s\n  IdentityFile %s\n  IdentitiesOnly yes\n  IdentityAgent none\n  BatchMode yes\n  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n  GlobalKnownHostsFile /dev/null\n", env.sshdPort, os.Getenv("USER"), strconv.Quote(key))
	if err := os.WriteFile(sshConfig, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skipf("ssh unavailable: %v", err)
	}
	ssh, err = filepath.Abs(ssh)
	if err != nil {
		t.Fatal(err)
	}
	// OpenSSH gets its default home from getpwuid, not HOME. This test-only
	// exec wrapper forces the SAME isolated config that the policy scans and
	// never falls back to ~/.ssh/config. exec preserves the supervised PID.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	sshUsed := filepath.Join(root, "ssh-pid")
	wrapper := filepath.Join(root, "ssh")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$$\" > %s\nexec %s -F %s \"$@\"\n", quote(sshUsed), quote(ssh), quote(sshConfig))
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tunnels.toml")
	saved := model.Tunnel{
		ID: "saved", Name: "Saved private SSH", Type: model.TypeSSH,
		SSH:      &model.SSH{Host: "mcp-test", Flags: []string{"-N"}},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: fmt.Sprintf("127.0.0.1:%d", freePort(t)), Target: env.echoAddr}},
	}
	if err := config.Save(path, model.Config{Tunnels: []model.Tunnel{saved}}); err != nil {
		t.Fatal(err)
	}
	agent := settings.DefaultAgent()
	agent.AllowSSHHosts = []string{"mcp-test"}
	agent.AllowTunnels = []string{"saved"}
	settingsPath := filepath.Join(root, "settings.toml")
	if err := settings.Save(settingsPath, settings.Settings{Agent: &agent, Tools: map[string]string{"ssh": wrapper}}); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	trail, err := audit.Open(state, audit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { trail.Close() })
	clock := &mcpClock{now: time.Now().UTC()}
	e, err := engine.New(engine.Options{
		Version: "test", ConfigPath: path, SettingsPath: settingsPath, StateDir: state,
		Secrets: &secret.Memory{}, AskpassHelper: buildHelper(t), Audit: trail, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	// The shared sshd helper uses a short temp path; keep Unix socket names
	// short too, independent of the test runner's long temporary directories.
	socket := filepath.Join(env.dir, "mcp.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := control.New(e, "test", nil)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("control serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("control server did not exit")
		}
	})
	return mcpSSHFixture{env: env, engine: e, clock: clock, socket: socket, state: state, sshUsed: sshUsed}
}

func (f mcpSSHFixture) connect(t *testing.T) (*mcp.ClientSession, *mcpserver.Adapter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	adapter := mcpserver.New(mcpserver.Options{Socket: f.socket, NoStart: true, Version: "test"})
	t.Cleanup(func() { adapter.Close() })
	ct, st := mcp.NewInMemoryTransports()
	serverSession, err := adapter.Server().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "private-sshd-e2e", Version: "test"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session, adapter
}

func mcpTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s tool error: %+v (%+v)", name, result.StructuredContent, result.Content)
	}
	out, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s structured content is %T, want object", name, result.StructuredContent)
	}
	return out
}

func (f mcpSSHFixture) openArgs(request string) map[string]any {
	return map[string]any{
		"request_id": request, "purpose": "Verify private SSH echo forwarding", "wait_seconds": 20,
		"spec": map[string]any{"type": "ssh", "host": "mcp-test", "forwards": []any{
			map[string]any{"kind": "L", "listen_port": "auto", "target": f.env.echoAddr},
		}},
	}
}

func mcpReady(t *testing.T, result map[string]any) (tunnelID, listen string) {
	t.Helper()
	if result["status"] != "ready" {
		t.Fatalf("open did not become ready: %+v", result)
	}
	tunnelID, _ = result["tunnel_id"].(string)
	endpoints, ok := result["endpoints"].([]any)
	if tunnelID == "" || !ok || len(endpoints) != 1 {
		t.Fatalf("missing tunnel or endpoint: %+v", result)
	}
	endpoint, ok := endpoints[0].(map[string]any)
	if !ok {
		t.Fatalf("endpoint is %T, want object", endpoints[0])
	}
	listen, _ = endpoint["listen"].(string)
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" || port == "auto" {
		t.Fatalf("not an allocated loopback endpoint: %q (%v)", listen, err)
	}
	if got, err := echoThrough(listen); err != nil || got != "ping" {
		t.Fatalf("MCP SSH echo: %q, %v", got, err)
	}
	return tunnelID, listen
}

func (f mcpSSHFixture) running(t *testing.T, id string, held, pinned bool) supervisor.Status {
	t.Helper()
	for _, st := range f.engine.Statuses() {
		if st.ID == id {
			if st.State != supervisor.StateRunning || st.PID <= 0 || (st.Owner.Holder != nil) != held || st.Owner.Pinned != pinned {
				t.Fatalf("unexpected running ownership: %+v", st)
			}
			if _, err := platform.ProcessIdentity(st.PID); err != nil {
				t.Fatalf("SSH process %d missing: %v", st.PID, err)
			}
			return st
		}
	}
	t.Fatalf("running tunnel %s missing: %+v", id, f.engine.Statuses())
	return supervisor.Status{}
}

func (f mcpSSHFixture) stopped(t *testing.T, id, listen string, pid int) {
	t.Helper()
	for _, st := range f.engine.Statuses() {
		if st.ID == id && (st.State != supervisor.StateStopped || st.PID != 0) {
			t.Fatalf("tunnel remains active after release: %+v", st)
		}
	}
	for _, st := range f.engine.Statuses() {
		if st.ID == id && st.Owner.Holder != nil {
			t.Fatalf("holder remains after release: %+v", st.Owner)
		}
	}
	if _, err := platform.ProcessIdentity(pid); err == nil {
		t.Fatalf("SSH process %d survived release", pid)
	}
	if c, err := net.DialTimeout("tcp", listen, time.Second); err == nil {
		c.Close()
		t.Fatalf("forward %s survived release", listen)
	}
}

// holder returns the client that holds a tunnel; agent holds never expire.
func (f mcpSSHFixture) holder(t *testing.T, id string) api.Actor {
	t.Helper()
	for _, st := range f.engine.Statuses() {
		if st.ID == id && st.Owner.Holder != nil {
			return *st.Owner.Holder
		}
	}
	t.Fatalf("no holder on %s", id)
	return api.Actor{}
}

func TestMCPSSHLifecycle(t *testing.T) {
	f := newMCPSSHFixture(t)
	t.Run("open retry close", func(t *testing.T) {
		session, _ := f.connect(t)
		args := f.openArgs("private-ssh-lifecycle")
		id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", args))
		st := f.running(t, id, true, false)
		if holder := f.holder(t, id); holder.Kind != "mcp" {
			t.Fatalf("agent must hold its tunnel: %+v", holder)
		}
		// The wrapper must have exec'd this very SSH process, not merely been
		// configured as a preview tool or invoked for an unrelated SSH query.
		pid, err := os.ReadFile(f.sshUsed)
		if err != nil || string(pid) != strconv.Itoa(st.PID) {
			t.Fatalf("isolated SSH wrapper not used for PID %d: %q, %v", st.PID, pid, err)
		}
		againID, againListen := mcpReady(t, mcpTool(t, session, "tunnel_open", args))
		if againID != id || againListen != listen || f.running(t, id, true, false).PID != st.PID {
			t.Fatalf("same request spawned another process: %s/%s", againID, againListen)
		}
		// Time passing never stops an agent tunnel.
		f.clock.advance(48 * time.Hour)
		if err := f.engine.Maintain(); err != nil {
			t.Fatal(err)
		}
		f.running(t, id, true, false)
		got := mcpTool(t, session, "tunnel_get", map[string]any{"tunnel_id": id})
		if owner, _ := got["owner"].(map[string]any); owner["opened_via_mcp"] != true || owner["client"] == "" {
			t.Fatalf("agent marker missing: %+v", got["owner"])
		}
		closed := mcpTool(t, session, "tunnel_close", map[string]any{"tunnel_id": id})
		if closed["stopped"] != true {
			t.Fatalf("close did not report stopped: %+v", closed)
		}
		f.stopped(t, id, listen, st.PID)
		mcpTool(t, session, "tunnel_close", map[string]any{"tunnel_id": id})
		batch, err := audit.NewReader(f.state).Read(0, audit.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []audit.Action{audit.TunnelRunEphemeral, audit.TunnelRelease} {
			found := false
			for _, event := range batch.Events {
				if event.Action == action && event.TunnelID == id && event.Actor.Kind == audit.ActorMCP {
					found = true
				}
			}
			if !found {
				t.Errorf("missing real MCP audit event %s for %s", action, id)
			}
		}
	})

	t.Run("person pinned saved tunnel survives close", func(t *testing.T) {
		if err := f.engine.Start("saved"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.engine.Stop("saved") })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := f.engine.WaitReady(ctx, "saved"); err != nil {
			t.Fatalf("start pinned saved SSH: %v, logs=%v", err, f.engine.Logs("saved"))
		}
		st := f.running(t, "saved", false, true)
		session, _ := f.connect(t)
		id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", map[string]any{
			"request_id": "pinned-saved", "purpose": "Borrow the person's existing SSH forward", "tunnel_id": "saved", "wait_seconds": 20,
		}))
		f.running(t, id, false, true)
		closed := mcpTool(t, session, "tunnel_close", map[string]any{"tunnel_id": id})
		if closed["stopped"] != false || f.running(t, id, false, true).PID != st.PID {
			t.Fatalf("closing borrowed tunnel stopped pinned process: %+v", closed)
		}
		if got, err := echoThrough(listen); err != nil || got != "ping" {
			t.Fatalf("pinned forward stopped: %q, %v", got, err)
		}
		if err := f.engine.StopTunnel(id); err != nil {
			t.Fatal(err)
		}
		f.stopped(t, id, listen, st.PID)
	})

	t.Run("two agents share a saved tunnel, close stops it", func(t *testing.T) {
		first, _ := f.connect(t)
		second, _ := f.connect(t)
		args := map[string]any{"request_id": "shared-saved", "purpose": "Share saved private SSH", "tunnel_id": "saved", "wait_seconds": 20}
		id, listen := mcpReady(t, mcpTool(t, first, "tunnel_open", args))
		st := f.running(t, id, true, false)
		// The same request ID in another adapter is scoped to a different
		// daemon session, even though initialize uses the same display name.
		id2, listen2 := mcpReady(t, mcpTool(t, second, "tunnel_open", args))
		if id2 != id || listen2 != listen || f.running(t, id, true, false).PID != st.PID {
			t.Fatalf("saved process not shared: %s/%s", id, id2)
		}
		closed := mcpTool(t, first, "tunnel_close", map[string]any{"tunnel_id": id})
		if closed["stopped"] != true {
			t.Fatalf("close should stop the tunnel for every agent: %+v", closed)
		}
		f.stopped(t, id, listen, st.PID)
	})

	t.Run("adapter disconnect leaves tunnel running until closed", func(t *testing.T) {
		session, adapter := f.connect(t)
		id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", f.openArgs("disconnect-private-ssh")))
		st := f.running(t, id, true, false)
		// Closing the daemon connection models an adapter process dying. There
		// is no grace period and no cleanup: the tunnel stays, marked as an
		// agent's, until someone closes it.
		if err := adapter.Close(); err != nil {
			t.Fatal(err)
		}
		f.clock.advance(time.Hour)
		if err := f.engine.Maintain(); err != nil {
			t.Fatal(err)
		}
		f.running(t, id, true, false)
		other, _ := f.connect(t)
		closed := mcpTool(t, other, "tunnel_close", map[string]any{"tunnel_id": id})
		if closed["stopped"] != true {
			t.Fatalf("a later agent session could not close it: %+v", closed)
		}
		f.stopped(t, id, listen, st.PID)
	})
}
