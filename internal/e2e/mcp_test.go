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
		"request_id": request, "purpose": "Verify private SSH echo forwarding", "wait_seconds": 20, "ttl_seconds": 60,
		"spec": map[string]any{"type": "ssh", "host": "mcp-test", "forwards": []any{
			map[string]any{"kind": "L", "listen_port": "auto", "target": f.env.echoAddr},
		}},
	}
}

func mcpReady(t *testing.T, result map[string]any) (lease, tunnelID, listen string) {
	t.Helper()
	if result["status"] != "ready" {
		t.Fatalf("open did not become ready: %+v", result)
	}
	lease, _ = result["lease_id"].(string)
	tunnelID, _ = result["tunnel_id"].(string)
	endpoints, ok := result["endpoints"].([]any)
	if lease == "" || tunnelID == "" || !ok || len(endpoints) != 1 {
		t.Fatalf("missing lease, tunnel, or endpoint: %+v", result)
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
	return lease, tunnelID, listen
}

func (f mcpSSHFixture) running(t *testing.T, id string, leases int, pinned bool) supervisor.Status {
	t.Helper()
	for _, st := range f.engine.Statuses() {
		if st.ID == id {
			if st.State != supervisor.StateRunning || st.PID <= 0 || st.Owner.Leases != leases || st.Owner.Pinned != pinned {
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
	for _, lease := range f.engine.ListLeases(api.Actor{}, "", false) {
		if lease.TunnelID == id && !lease.Revoked {
			t.Fatalf("live lease remains after release: %+v", lease)
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

func (f mcpSSHFixture) lease(t *testing.T, id string) api.Lease {
	t.Helper()
	for _, lease := range f.engine.ListLeases(api.Actor{}, "", false) {
		if lease.ID == id {
			return lease
		}
	}
	t.Fatalf("lease %s not found", id)
	return api.Lease{}
}

func TestMCPSSHLifecycle(t *testing.T) {
	f := newMCPSSHFixture(t)
	t.Run("open retry renew close", func(t *testing.T) {
		session, _ := f.connect(t)
		args := f.openArgs("private-ssh-lifecycle")
		lease, id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", args))
		st := f.running(t, id, 1, false)
		// The wrapper must have exec'd this very SSH process, not merely been
		// configured as a preview tool or invoked for an unrelated SSH query.
		pid, err := os.ReadFile(f.sshUsed)
		if err != nil || string(pid) != strconv.Itoa(st.PID) {
			t.Fatalf("isolated SSH wrapper not used for PID %d: %q, %v", st.PID, pid, err)
		}
		again, againID, againListen := mcpReady(t, mcpTool(t, session, "tunnel_open", args))
		if again != lease || againID != id || againListen != listen || f.running(t, id, 1, false).PID != st.PID {
			t.Fatalf("same request spawned another lease/process: %s/%s/%s", again, againID, againListen)
		}
		before := f.lease(t, lease)
		f.clock.advance(5 * time.Second)
		renewed := mcpTool(t, session, "tunnel_renew", map[string]any{"lease_id": lease, "ttl_seconds": 120})
		after := f.lease(t, lease)
		if renewed["lease_id"] != lease || !after.ExpiresAt.Equal(f.clock.Now().Add(120*time.Second)) || !after.ExpiresAt.After(before.ExpiresAt) {
			t.Fatalf("lease not renewed: before=%+v after=%+v result=%+v", before, after, renewed)
		}
		if f.running(t, id, 1, false).PID != st.PID {
			t.Fatal("renewal replaced the SSH process")
		}
		closed := mcpTool(t, session, "tunnel_close", map[string]any{"lease_id": lease})
		if closed["stopped"] != true {
			t.Fatalf("close did not report stopped: %+v", closed)
		}
		f.stopped(t, id, listen, st.PID)
		mcpTool(t, session, "tunnel_close", map[string]any{"lease_id": lease})
		batch, err := audit.NewReader(f.state).Read(0, audit.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []audit.Action{audit.LeaseOpen, audit.LeaseRenew, audit.LeaseClose} {
			found := false
			for _, event := range batch.Events {
				if event.Action == action && event.LeaseID == lease && event.Actor.Kind == audit.ActorMCP {
					found = true
				}
			}
			if !found {
				t.Errorf("missing real MCP audit event %s for %s", action, lease)
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
		st := f.running(t, "saved", 0, true)
		session, _ := f.connect(t)
		lease, id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", map[string]any{
			"request_id": "pinned-saved", "purpose": "Borrow the person's existing SSH forward", "tunnel_id": "saved", "wait_seconds": 20,
		}))
		f.running(t, id, 1, true)
		closed := mcpTool(t, session, "tunnel_close", map[string]any{"lease_id": lease})
		if closed["stopped"] != false || f.running(t, id, 0, true).PID != st.PID {
			t.Fatalf("closing borrowed lease stopped pinned process: %+v", closed)
		}
		if got, err := echoThrough(listen); err != nil || got != "ping" {
			t.Fatalf("pinned forward stopped: %q, %v", got, err)
		}
		if err := f.engine.StopTunnel(id); err != nil {
			t.Fatal(err)
		}
		f.stopped(t, id, listen, st.PID)
	})

	t.Run("two leases share saved tunnel", func(t *testing.T) {
		first, _ := f.connect(t)
		second, _ := f.connect(t)
		args := map[string]any{"request_id": "shared-saved", "purpose": "Share saved private SSH", "tunnel_id": "saved", "wait_seconds": 20}
		lease1, id, listen := mcpReady(t, mcpTool(t, first, "tunnel_open", args))
		st := f.running(t, id, 1, false)
		// The same request ID in another adapter is scoped to a different
		// daemon session, even though initialize uses the same display name.
		lease2, id2, listen2 := mcpReady(t, mcpTool(t, second, "tunnel_open", args))
		if lease1 == lease2 || id2 != id || listen2 != listen || f.running(t, id, 2, false).PID != st.PID {
			t.Fatalf("saved process not shared: %s/%s %s/%s", lease1, lease2, id, id2)
		}
		closed := mcpTool(t, first, "tunnel_close", map[string]any{"lease_id": lease1})
		if closed["stopped"] != false || f.running(t, id, 1, false).PID != st.PID {
			t.Fatalf("first close stopped another holder's process: %+v", closed)
		}
		if got, err := echoThrough(listen); err != nil || got != "ping" {
			t.Fatalf("shared forward stopped: %q, %v", got, err)
		}
		mcpTool(t, second, "tunnel_close", map[string]any{"lease_id": lease2})
		f.stopped(t, id, listen, st.PID)
	})

	t.Run("TTL expiry stops private SSH", func(t *testing.T) {
		session, _ := f.connect(t)
		lease, id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", f.openArgs("expire-private-ssh")))
		st := f.running(t, id, 1, false)
		f.clock.advance(f.lease(t, lease).ExpiresAt.Sub(f.clock.Now()) - time.Nanosecond)
		if err := f.engine.ExpireLeases(); err != nil {
			t.Fatal(err)
		}
		f.running(t, id, 1, false)
		f.clock.advance(time.Nanosecond)
		if err := f.engine.ExpireLeases(); err != nil {
			t.Fatal(err)
		}
		f.stopped(t, id, listen, st.PID)
	})

	t.Run("adapter disconnect grace stops private SSH", func(t *testing.T) {
		session, adapter := f.connect(t)
		args := f.openArgs("disconnect-private-ssh")
		args["ttl_seconds"] = 3600 // Isolate disconnect grace from TTL expiry.
		lease, id, listen := mcpReady(t, mcpTool(t, session, "tunnel_open", args))
		st := f.running(t, id, 1, false)
		// Closing the daemon connection models an adapter process dying: do
		// not call tunnel_close, and retain the MCP session while it drops.
		if err := adapter.Close(); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var claim api.Lease
		for claim.GraceUntil == nil {
			claim = f.lease(t, lease)
			if claim.GraceUntil != nil {
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("adapter disconnect did not start grace")
			case <-ticker.C:
			}
		}
		if !claim.GraceUntil.Equal(f.clock.Now().Add(engine.DisconnectGrace)) {
			t.Fatalf("unexpected disconnect deadline: %+v", claim)
		}
		f.clock.advance(engine.DisconnectGrace - time.Nanosecond)
		if err := f.engine.ExpireLeases(); err != nil {
			t.Fatal(err)
		}
		f.running(t, id, 1, false)
		f.clock.advance(time.Nanosecond)
		if err := f.engine.ExpireLeases(); err != nil {
			t.Fatal(err)
		}
		f.stopped(t, id, listen, st.PID)
	})
}
