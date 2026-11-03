// Package e2e runs a real ssh tunnel through the real supervisor, askpass helper and process runner.
package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const passphrase = "correct horse"

type sshEnv struct {
	dir      string
	key      string
	sshdPort int
	echoAddr string
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// setup starts an unprivileged sshd and a TCP echo server behind it.
func setup(t *testing.T) sshEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs an unprivileged sshd")
	}
	sshd := "/usr/sbin/sshd"
	if _, err := os.Stat(sshd); err != nil {
		t.Skip("sshd not installed")
	}
	dir, err := os.MkdirTemp("", "dtte")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	hostKey := filepath.Join(dir, "host")
	userKey := filepath.Join(dir, "user")
	run(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", hostKey)
	run(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-f", userKey)

	port := freePort(t)
	conf := filepath.Join(dir, "sshd_config")
	content := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s\nAuthorizedKeysFile %s.pub\nPidFile %s/pid\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowTcpForwarding yes\n",
		port, hostKey, userKey, dir)
	if err := os.WriteFile(conf, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sshd, "-D", "-e", "-f", conf)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sshd: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", port))

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				c.Write(buf[:n])
			}()
		}
	}()
	return sshEnv{dir: dir, key: userKey, sshdPort: port, echoAddr: echo.Addr().String()}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Skipf("%s: %v: %s", name, err, out)
	}
}

func waitListening(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Skipf("sshd did not listen on %s", addr)
}

func buildHelper(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "devt-tunnel-askpass")
	cmd := exec.Command("go", "build", "-o", out, "github.com/trieu04/devt-tunnel/cmd/devt-tunnel-askpass")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, b)
	}
	return out
}

func newManager(t *testing.T, store secret.Store, ask askpass.Asker, tun model.Tunnel) *supervisor.Manager {
	t.Helper()
	broker, err := askpass.NewBroker(buildHelper(t), store, ask)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broker.Close() })
	tools := &platform.Tools{}
	m := supervisor.New(supervisor.Deps{
		Runner:       platform.Runner{},
		Secrets:      store,
		Resolve:      tools.Resolve,
		Environ:      tools.Environ,
		Askpass:      broker,
		ReadyTimeout: 20 * time.Second,
		StopGrace:    2 * time.Second,
	})
	m.SetConfig(model.Config{Tunnels: []model.Tunnel{tun}})
	t.Cleanup(m.Shutdown)
	return m
}

func tunnel(env sshEnv, localPort int) model.Tunnel {
	return model.Tunnel{
		ID: "t", Name: "t", Type: model.TypeSSH,
		SSH: &model.SSH{
			User: os.Getenv("USER"), Hostname: "127.0.0.1", Port: env.sshdPort, Identity: env.key,
			Flags: []string{"-N", "-F", "/dev/null"},
			Options: map[string]string{
				"StrictHostKeyChecking": "no",
				"UserKnownHostsFile":    "/dev/null",
				"IdentitiesOnly":        "yes",
				"IdentityAgent":         "none",
			},
			PassphraseSecret: "key",
		},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: fmt.Sprintf("127.0.0.1:%d", localPort), Target: env.echoAddr}},
	}
}

func waitState(t *testing.T, m *supervisor.Manager, want supervisor.State) supervisor.Status {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	var st supervisor.Status
	for time.Now().Before(deadline) {
		st = m.Statuses()[0]
		if st.State == want {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state %s, want %s (%s)\nlog:\n%s", st.State, want, st.Error, strings.Join(m.Logs("t"), "\n"))
	return st
}

func echoThrough(addr string) (string, error) {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		return "", err
	}
	buf := make([]byte, 16)
	n, err := c.Read(buf)
	return string(buf[:n]), err
}

func TestSSHTunnelWithStoredPassphrase(t *testing.T) {
	env := setup(t)
	store := &secret.Memory{}
	store.Set("key", passphrase)
	var asked atomic.Int32
	ask := func(context.Context, askpass.Prompt) (string, error) {
		asked.Add(1)
		return "", fmt.Errorf("should not ask")
	}
	local := freePort(t)
	m := newManager(t, store, ask, tunnel(env, local))
	if err := m.Start("t"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, m, supervisor.StateRunning)
	addr := fmt.Sprintf("127.0.0.1:%d", local)
	if got, err := echoThrough(addr); err != nil || got != "ping" {
		t.Fatalf("through tunnel: %q, %v\nlog:\n%s", got, err, strings.Join(m.Logs("t"), "\n"))
	}
	if asked.Load() != 0 {
		t.Errorf("user was asked %d times", asked.Load())
	}
	if _, err := platform.ProcessIdentity(st.PID); err != nil {
		t.Errorf("ssh not running: %v", err)
	}

	m.Stop("t")
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Error("port still open after stop")
	}
	if _, err := platform.ProcessIdentity(st.PID); err == nil {
		t.Error("ssh still running after stop")
	}
}

func TestWrongStoredPassphraseFallsBackToUser(t *testing.T) {
	env := setup(t)
	store := &secret.Memory{}
	store.Set("key", "wrong value")
	var prompts atomic.Int32
	ask := func(_ context.Context, p askpass.Prompt) (string, error) {
		prompts.Add(1)
		if p.Kind != askpass.KindPassphrase {
			return "", fmt.Errorf("unexpected prompt %q", p.Text)
		}
		return passphrase, nil
	}
	m := newManager(t, store, ask, tunnel(env, freePort(t)))
	m.Start("t")
	waitState(t, m, supervisor.StateRunning)
	if prompts.Load() != 1 {
		t.Errorf("user asked %d times, want 1", prompts.Load())
	}
}

func TestSSHForwardFailureIsFailed(t *testing.T) {
	env := setup(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	store := &secret.Memory{}
	store.Set("key", passphrase)
	m := newManager(t, store, func(context.Context, askpass.Prompt) (string, error) { return "", fmt.Errorf("no") },
		tunnel(env, busy.Addr().(*net.TCPAddr).Port))
	m.Start("t")
	st := waitState(t, m, supervisor.StateFailed)
	if !strings.Contains(st.Error, "already in use") {
		t.Errorf("error %q", st.Error)
	}
}
