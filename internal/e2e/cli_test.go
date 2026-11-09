package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// Exercise the shipping CLI binary, not a Go client standing in for commands.
// An unencrypted throwaway key avoids depending on a desktop credential store.
func TestCLISSHLifecycleAndPrompts(t *testing.T) {
	env := setup(t)
	run(t, "ssh-keygen", "-q", "-p", "-P", passphrase, "-N", "", "-f", env.key)
	bin := t.TempDir()
	cliBin := filepath.Join(bin, "devt-tunnel")
	for _, build := range []struct{ output, pkg string }{
		{cliBin, "../../cmd/devt-tunnel"},
		{filepath.Join(bin, "devt-tunnel-askpass"), "../../cmd/devt-tunnel-askpass"},
	} {
		if output, err := exec.Command("go", "build", "-o", build.output, build.pkg).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", build.pkg, err, output)
		}
	}
	root := t.TempDir()
	runDir, err := os.MkdirTemp("", "dtt-cli-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	socket := filepath.Join(runDir, "ctl.sock")
	for key, value := range map[string]string{
		"HOME": root, "USERPROFILE": root, "SHELL": "", "DTT_SOCKET": socket,
		"DEVT_TUNNEL_CONFIG_DIR": filepath.Join(root, "config"),
		"DEVT_TUNNEL_STATE_DIR":  filepath.Join(root, "state"),
	} {
		t.Setenv(key, value)
	}
	local := freePort(t)
	tun := tunnel(env, local)
	tun.SSH.PassphraseSecret = ""
	tun.SSH.Options["StrictHostKeyChecking"] = "ask"
	knownHosts := filepath.Join(root, "known_hosts")
	tun.SSH.Options["UserKnownHostsFile"] = knownHosts
	tun.SSH.Options["GlobalKnownHostsFile"] = "/dev/null"
	if runtime.GOOS == "darwin" {
		tun.SSH.Options["UseKeychain"] = "no"
	}
	if err := config.Save(filepath.Join(root, "config", "tunnels.toml"), model.Config{Tunnels: []model.Tunnel{tun}}); err != nil {
		t.Fatal(err)
	}
	daemon := exec.Command(cliBin, "daemon", "run", "--idle-exit", "0")
	var daemonOutput bytes.Buffer
	daemon.Stdout, daemon.Stderr = &daemonOutput, &daemonOutput
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Wait() }()
	t.Cleanup(func() {
		select {
		case <-daemonDone:
		default:
			_ = daemon.Process.Kill()
			<-daemonDone
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var c *client.Client
	for {
		c, err = client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "cli-e2e", Version: "dev"})
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("daemon did not start: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = c.Call(cleanup, api.DaemonShutdown, api.ShutdownParams{}, nil)
	})
	invoke := func(input string, args ...string) (int, string, string) {
		t.Helper()
		command := exec.CommandContext(ctx, cliBin, append([]string{"--no-daemon-start", "--timeout", "15s"}, args...)...)
		command.Stdin = strings.NewReader(input)
		var out, diagnostics bytes.Buffer
		command.Stdout, command.Stderr = &out, &diagnostics
		err := command.Run()
		if err == nil {
			return 0, out.String(), diagnostics.String()
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), out.String(), diagnostics.String()
		}
		t.Fatalf("%v: %v", args, err)
		return 1, "", ""
	}
	address := fmt.Sprintf("127.0.0.1:%d", local)
	verifyAndStop := func(t *testing.T) {
		t.Helper()
		if got, err := echoThrough(address); err != nil || got != "ping" {
			t.Fatalf("forward=%q error=%v", got, err)
		}
		if code, out, diagnostics := invoke("", "start", "t"); code != 0 || strings.TrimSpace(out) != address {
			t.Fatalf("idempotent start: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if code, out, diagnostics := invoke("", "logs", "t", "--json"); code != 0 || !strings.Contains(out, `"apiVersion":2`) || !strings.Contains(out, `"lines":`) {
			t.Fatalf("logs: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if code, out, diagnostics := invoke("", "stop", "t", "--json"); code != 0 || !strings.Contains(out, `"state":"stopped"`) {
			t.Fatalf("stop: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if _, err := echoThrough(address); err == nil {
			t.Fatal("forward survived stop")
		}
	}
	t.Run("answer-from-second-process", func(t *testing.T) {
		if code, out, diagnostics := invoke("", "start", "t", "--json"); code != 4 || out != "" || !strings.Contains(diagnostics, "devt-tunnel prompts") {
			t.Fatalf("non-TTY start: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		code, out, diagnostics := invoke("", "prompts", "--json")
		var result struct{ Prompts []api.PendingPrompt }
		if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || len(result.Prompts) != 1 || result.Prompts[0].Prompt.Kind != "host_key" {
			t.Fatalf("prompts: code=%d stdout=%s stderr=%s decode=%v", code, out, diagnostics, err)
		}
		if code, out, diagnostics := invoke("yes\n", "prompts", "answer", result.Prompts[0].ID, "--stdin", "--json"); code != 0 {
			t.Fatalf("answer: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if code, out, diagnostics := invoke("", "wait", "t"); code != 0 || strings.TrimSpace(out) != address {
			t.Fatalf("wait: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		verifyAndStop(t)
	})
	t.Run("port-conflict-exits-five", func(t *testing.T) {
		busy, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		defer busy.Close()
		if code, out, diagnostics := invoke("", "start", "t", "--json"); code != 5 || out != "" || !strings.Contains(diagnostics, `"code":"port_in_use"`) {
			t.Fatalf("port conflict: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if code, out, diagnostics := invoke("", "stop", "t"); code != 0 {
			t.Fatalf("stop after conflict: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
	})
	t.Run("inline-host-key-on-tty", func(t *testing.T) {
		if err := os.Remove(knownHosts); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		output, err := cliPTYStart(t, ctx, cliBin, "continue connecting", "yes\n")
		if err != nil || !strings.Contains(output, address) {
			t.Fatalf("TTY start: %v\n%s", err, output)
		}
		verifyAndStop(t)
	})
	t.Run("ephemeral-forward-connect-stop", func(t *testing.T) {
		sshConfig := filepath.Join(root, "forward_ssh_config")
		contents := fmt.Sprintf("Host forward-fixture\n  HostName 127.0.0.1\n  User %s\n  Port %d\n  IdentityFile %q\n  IdentitiesOnly yes\n  IdentityAgent none\n  StrictHostKeyChecking yes\n  UserKnownHostsFile %q\n  GlobalKnownHostsFile /dev/null\n", os.Getenv("USER"), env.sshdPort, env.key, knownHosts)
		if err := os.WriteFile(sshConfig, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, out, diagnostics := invoke("", "profile", "add", "forward-fixture", "--ssh-config", sshConfig); code != 0 {
			t.Fatalf("profile add: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		code, out, diagnostics := invoke("", "forward", "forward-fixture", "auto:"+env.echoAddr, "--profile", "forward-fixture", "--ttl", "5m", "--json")
		var result struct {
			APIVersion int `json:"apiVersion"`
			Tunnels    []struct {
				ID        string
				Endpoints []api.Endpoint
			}
		}
		if err := json.Unmarshal([]byte(out), &result); code != 0 || err != nil || result.APIVersion != api.Version || len(result.Tunnels) != 1 || len(result.Tunnels[0].Endpoints) != 1 {
			t.Fatalf("forward: code=%d stdout=%s stderr=%s decode=%v", code, out, diagnostics, err)
		}
		id, address := result.Tunnels[0].ID, result.Tunnels[0].Endpoints[0].Listen
		if !strings.HasPrefix(id, "tmp-") {
			t.Fatalf("ephemeral ID=%q", id)
		}
		// The launching CLI has exited: a detached lease still owns the forward.
		if got, err := echoThrough(address); err != nil || got != "ping" {
			t.Fatalf("ephemeral connection=%q error=%v", got, err)
		}
		if code, out, diagnostics := invoke("", "stop", id, "--json"); code != 0 || !strings.Contains(out, `"state":"stopped"`) {
			t.Fatalf("ephemeral stop: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if _, err := echoThrough(address); err == nil {
			t.Fatal("ephemeral forward survived stop")
		}
	})
	t.Run("hidden-passphrase-on-tty", func(t *testing.T) {
		run(t, "ssh-keygen", "-q", "-p", "-P", "", "-N", passphrase, "-f", env.key)
		output, err := cliPTYStart(t, ctx, cliBin, "passphrase for", passphrase+"\n")
		if err != nil || !strings.Contains(output, address) || strings.Contains(output, passphrase) {
			t.Fatalf("hidden TTY start: %v\n%s", err, output)
		}
		verifyAndStop(t)
	})
	t.Run("ctrl-c-cancels-hidden-question", func(t *testing.T) {
		output, _ := cliPTYStart(t, ctx, cliBin, "passphrase for", "\x03")
		if !strings.Contains(output, "context canceled") || strings.Contains(output, address) {
			t.Fatalf("interrupted TTY start: %s", output)
		}
		code, out, diagnostics := invoke("", "prompts", "--json")
		if code != 0 || !strings.Contains(out, `"prompts":[]`) {
			t.Fatalf("cancel left a pending question: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
		if code, out, diagnostics := invoke("", "stop", "t"); code != 0 {
			t.Fatalf("stop after cancel: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
	})
}

func cliPTYStart(t *testing.T, ctx context.Context, cliBin, question, answer string) (string, error) {
	t.Helper()
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("native script utility unavailable; second-process --stdin path is covered")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("native script invocation is only defined for macOS and Linux")
	}
	var args []string
	if runtime.GOOS == "darwin" {
		args = []string{"-q", "/dev/null", cliBin, "--no-daemon-start", "start", "t", "--timeout", "15s"}
	} else {
		quoted := "'" + strings.ReplaceAll(cliBin, "'", "'\\''") + "'"
		args = []string{"-q", "-e", "-c", "SHELL= exec " + quoted + " --no-daemon-start start t --timeout 15s", "/dev/null"}
	}
	command := exec.CommandContext(ctx, script, args...)
	command.Env = append(os.Environ(), "SHELL=/bin/sh")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	output := &cliPTYOutput{ready: make(chan struct{}), question: question}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-output.ready:
		if _, err := stdin.Write([]byte(answer)); err != nil {
			t.Fatal(err)
		}
	case err := <-done:
		t.Fatalf("TTY command exited before asking: %v\n%s", err, output.String())
	case <-ctx.Done():
		t.Fatalf("TTY command never asked: %s", output.String())
	}
	select {
	case err := <-done:
		return output.String(), err
	case <-ctx.Done():
		t.Fatalf("TTY command hung: %s", output.String())
		return "", ctx.Err()
	}
}

// script owns a real pseudoterminal without adding a Go PTY dependency. Wait for
// the actual question before typing; an early EOF can preempt input on macOS.
type cliPTYOutput struct {
	mu       sync.Mutex
	text     bytes.Buffer
	once     sync.Once
	ready    chan struct{}
	question string
}

func (out *cliPTYOutput) Write(p []byte) (int, error) {
	out.mu.Lock()
	defer out.mu.Unlock()
	n, err := out.text.Write(p)
	if strings.Contains(strings.ToLower(out.text.String()), out.question) {
		out.once.Do(func() { close(out.ready) })
	}
	return n, err
}

func (out *cliPTYOutput) String() string {
	out.mu.Lock()
	defer out.mu.Unlock()
	return out.text.String()
}
