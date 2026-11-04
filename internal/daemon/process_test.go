package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

var fixture string
var oldFixture string

func TestMain(m *testing.M) {
	if binary := os.Getenv("DTT_CLIENT_PROCESS_HELPER"); binary != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := client.Ensure(ctx, client.EnsureOptions{Socket: os.Getenv("DTT_SOCKET"), Info: info, DaemonPath: binary})
		if err == nil {
			err = c.Call(ctx, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
		}
		if err == nil {
			err = c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: "sleep"}, nil)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "dtt-binary")
	if err != nil {
		panic(err)
	}
	fixture = filepath.Join(dir, "dtt")
	if runtime.GOOS == "windows" {
		fixture += ".exe"
	}
	cmd := exec.Command("go", "test", "-c", "-o", fixture, "../../cmd/dtt")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build dtt fixture: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	oldFixture = filepath.Join(dir, "dtt-old")
	if runtime.GOOS == "windows" {
		oldFixture += ".exe"
	}
	cmd = exec.Command("go", "test", "-c", "-ldflags=-X github.com/trieu04/devt-tunnel/cmd/dtt.version=older", "-o", oldFixture, "../../cmd/dtt")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build old dtt fixture: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type sandbox struct{ root, socket string }

var info = api.ClientInfo{Kind: "cli", Name: "process-test", Version: "dev"}

func isolated(t *testing.T) sandbox {
	t.Helper()
	root := t.TempDir()
	run, err := os.MkdirTemp("", "dttp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	socket := filepath.Join(run, "ctl.sock")
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "SHELL": "", "DTT_SOCKET": socket, "DEVT_TUNNEL_CONFIG_DIR": filepath.Join(root, "config"), "DEVT_TUNNEL_STATE_DIR": filepath.Join(root, "state"), "DTT_PROCESS_TEST": "1"} {
		t.Setenv(key, value)
	}
	return sandbox{root, socket}
}

func (s sandbox) save(t *testing.T) {
	t.Helper()
	line := "echo ready; sleep 120 & echo $! > " + strconv.Quote(filepath.Join(s.root, "child.pid")) + "; wait"
	if runtime.GOOS == "windows" {
		line = "echo ready & ping -n 120 127.0.0.1 > nul"
	}
	err := config.Save(filepath.Join(s.root, "config", "tunnels.toml"), model.Config{Tunnels: []model.Tunnel{{ID: "sleep", Name: "Sleep", Type: model.TypeCommand, Command: &model.Command{Line: line, ReadyLog: "ready"}}}})
	if err != nil {
		t.Fatal(err)
	}
}

type process struct {
	cmd  *exec.Cmd
	done chan error
}

func (s sandbox) start(t *testing.T, idle string) *process {
	return s.startBinary(t, fixture, idle)
}

func (s sandbox) startBinary(t *testing.T, binary, idle string) *process {
	t.Helper()
	cmd := exec.Command(binary, "daemon", "run", "--idle-exit", idle)
	log, err := os.CreateTemp(s.root, "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if c, err := client.DialManagement(ctx, s.socket, info); err == nil {
			c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, nil)
			c.Close()
		}
		select {
		case <-p.done:
		case <-ctx.Done():
			cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (s sandbox) dial(t *testing.T) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		c, err := client.Dial(ctx, s.socket, info)
		if err == nil {
			return c
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
			return nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func call(t *testing.T, c *client.Client, method string, params, result any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Call(ctx, method, params, result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}
func awaitExit(t *testing.T, p *process) {
	t.Helper()
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit")
	}
}

func TestTunnelSurvivesDisconnectAndShutdownStopsTree(t *testing.T) {
	s := isolated(t)
	s.save(t)
	p := s.start(t, "0")
	c := s.dial(t)
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	var status supervisor.Status
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, &status)
	c.Close()
	c = s.dial(t)
	defer c.Close()
	var view api.TunnelView
	call(t, c, "tunnel.get", api.IDParams{ID: "sleep"}, &view)
	if view.Status.PID != status.PID || view.Status.State != supervisor.StateRunning {
		t.Fatalf("lost running tunnel: %+v", view.Status)
	}
	var childPID int
	if runtime.GOOS != "windows" {
		for i := 0; i < 100; i++ {
			if data, err := os.ReadFile(filepath.Join(s.root, "child.pid")); err == nil {
				fmt.Sscanf(string(data), "%d", &childPID)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if childPID == 0 {
			t.Fatal("missing child pid")
		}
	}
	call(t, c, api.DaemonShutdown, api.ShutdownParams{}, nil)
	awaitExit(t, p)
	for _, pid := range []int{status.PID, childPID} {
		if pid > 0 {
			if _, err := platform.ProcessIdentity(pid); err == nil {
				t.Errorf("process %d survived shutdown", pid)
			}
		}
	}
}

func TestSecondDaemonAndConcurrentEnsure(t *testing.T) {
	s := isolated(t)
	const count = 6
	clients := make(chan *client.Client, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c, err := client.Ensure(ctx, client.EnsureOptions{Socket: s.socket, Info: info, DaemonPath: fixture})
			if err != nil {
				errs <- err
			} else {
				clients <- c
			}
		})
	}
	wg.Wait()
	close(clients)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	pid := 0
	var keeper *client.Client
	for c := range clients {
		if pid == 0 {
			pid = c.Hello().PID
			keeper = c
		} else {
			if c.Hello().PID != pid {
				t.Error("multiple daemon winners")
			}
			c.Close()
		}
	}
	if keeper == nil {
		t.Fatal("no daemon")
	}
	defer keeper.Close()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if c, err := client.DialManagement(ctx, s.socket, info); err == nil {
			defer c.Close()
			c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, nil)
		}
	})
	duplicate := exec.Command(fixture, "daemon", "run")
	if err := duplicate.Run(); err == nil {
		t.Fatal("second daemon succeeded")
	}
	call(t, keeper, api.DaemonStatusMethod, api.Empty{}, nil)
	call(t, keeper, api.DaemonShutdown, api.ShutdownParams{}, nil)
}

func TestIdleExitAndActiveTunnel(t *testing.T) {
	s := isolated(t)
	p := s.start(t, "250ms")
	c := s.dial(t)
	c.Close()
	awaitExit(t, p)
	s.save(t)
	p = s.start(t, "250ms")
	c = s.dial(t)
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, nil)
	c.Close()
	select {
	case <-p.done:
		t.Fatal("active daemon exited")
	case <-time.After(700 * time.Millisecond):
	}
	c = s.dial(t)
	call(t, c, "tunnel.stop", api.IDParams{ID: "sleep"}, nil)
	c.Close()
	awaitExit(t, p)
}

func TestCLIStatusAndStop(t *testing.T) {
	s := isolated(t)
	s.save(t)
	p := s.start(t, "0")
	c := s.dial(t)
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, nil)
	c.Close()
	out, err := exec.Command(fixture, "daemon", "status", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v %s", err, out)
	}
	var status api.DaemonStatus
	if json.Unmarshal(out, &status) != nil || status.PID != p.cmd.Process.Pid || len(status.Tunnels) != 1 || status.Tunnels[0].ID != "sleep" || status.Tunnels[0].State != supervisor.StateRunning {
		t.Fatalf("status: %s", out)
	}
	if out, err := exec.Command(fixture, "daemon", "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop: %v %s", err, out)
	}
	awaitExit(t, p)
}

func TestDaemonOutlivesClientProcess(t *testing.T) {
	s := isolated(t)
	s.save(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if c, err := client.DialManagement(ctx, s.socket, info); err == nil {
			defer c.Close()
			c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, nil)
		}
	})
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "DTT_CLIENT_PROCESS_HELPER="+fixture)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("client process: %v %s", err, out)
	}
	c := s.dial(t)
	defer c.Close()
	var view api.TunnelView
	call(t, c, "tunnel.get", api.IDParams{ID: "sleep"}, &view)
	if view.Status.State != supervisor.StateRunning {
		t.Fatalf("tunnel did not outlive client: %+v", view.Status)
	}
	if out, err := exec.Command(fixture, "daemon", "status").CombinedOutput(); err != nil {
		t.Fatalf("status: %v %s", err, out)
	}
	if out, err := exec.Command(fixture, "daemon", "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop: %v %s", err, out)
	}
	if _, err := platform.ProcessIdentity(view.Status.PID); err == nil {
		t.Fatal("tunnel survived daemon stop")
	}
}

func TestEnsureUpgradesIdleBuildAndKeepsActiveBuild(t *testing.T) {
	s := isolated(t)
	p := s.startBinary(t, oldFixture, "0")
	c := s.dial(t)
	if c.Hello().DaemonVersion != "older" {
		c.Close()
		t.Fatalf("old fixture has version %q", c.Hello().DaemonVersion)
	}
	c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var clients []*client.Client
	for i := 0; i < 3; i++ {
		c, err := client.Ensure(ctx, client.EnsureOptions{Socket: s.socket, Info: info, DaemonPath: fixture})
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
		defer c.Close()
		if c.Hello().DaemonVersion != "dev" || c.Hello().PID == p.cmd.Process.Pid {
			t.Fatalf("old build retained: %+v", c.Hello())
		}
	}
	call(t, clients[0], api.DaemonShutdown, api.ShutdownParams{}, nil)
	awaitExit(t, p)
	select {
	case <-clients[0].Done():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	s.save(t)
	p = s.startBinary(t, oldFixture, "0")
	c = s.dial(t)
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, nil)
	c.Close()
	c, err := client.Ensure(ctx, client.EnsureOptions{Socket: s.socket, Info: info, DaemonPath: fixture})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Hello().DaemonVersion != "older" || c.Hello().PID != p.cmd.Process.Pid {
		t.Fatalf("active build replaced: %+v", c.Hello())
	}
	call(t, c, api.DaemonShutdown, api.ShutdownParams{}, nil)
	awaitExit(t, p)
}
