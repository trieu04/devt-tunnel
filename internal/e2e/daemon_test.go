package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestSSHThroughDaemonWithStoredPassphraseAndPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires private unprivileged sshd")
	}
	env := setup(t)
	bin := t.TempDir()
	cliBin := filepath.Join(bin, "devt-tunnel")
	for _, args := range [][]string{{"test", "-c", "-o", cliBin, "../../cmd/devt-tunnel"}, {"build", "-o", filepath.Join(bin, "devt-tunnel-askpass"), "../../cmd/devt-tunnel-askpass"}} {
		if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v %s", err, output)
		}
	}
	root := t.TempDir()
	runDir, err := os.MkdirTemp("", "dtts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runDir)
	socket := filepath.Join(runDir, "ctl.sock")
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "SHELL": "", "DTT_SOCKET": socket, "DEVT_TUNNEL_CONFIG_DIR": filepath.Join(root, "config"), "DEVT_TUNNEL_STATE_DIR": filepath.Join(root, "state"), "DTT_PROCESS_TEST": "1"} {
		t.Setenv(key, value)
	}
	local := freePort(t)
	tun := tunnel(env, local)
	tun.SSH.Options["StrictHostKeyChecking"] = "ask"
	tun.SSH.Options["UserKnownHostsFile"] = filepath.Join(root, "known_hosts")
	tun.SSH.Options["GlobalKnownHostsFile"] = "/dev/null"
	if runtime.GOOS == "darwin" {
		tun.SSH.Options["UseKeychain"] = "no"
	}
	if err = config.Save(filepath.Join(root, "config", "tunnels.toml"), model.Config{Tunnels: []model.Tunnel{tun}}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cliBin, "daemon", "run", "--idle-exit", "0")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			cmd.Process.Kill()
			<-done
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var c *client.Client
	for {
		c, err = client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "ssh-e2e", Version: "dev"})
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer c.Close()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c.Call(cleanup, api.DaemonShutdown, api.ShutdownParams{}, nil)
	})
	if err = c.Call(ctx, "secret.set", api.SetSecretParams{Name: "key", Value: passphrase, Kind: secret.KindSSHPassphrase}, nil); err != nil {
		t.Fatal(err)
	}
	if err = c.Call(ctx, "tunnel.start", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	for {
		var prompts []api.PendingPrompt
		if err = c.Call(ctx, "prompt.list", api.Empty{}, &prompts); err != nil {
			t.Fatal(err)
		}
		if len(prompts) > 0 {
			if len(prompts) != 1 || prompts[0].Prompt.Kind != "host_key" {
				t.Fatalf("unexpected prompts: %+v", prompts)
			}
			if err = c.Call(ctx, "prompt.answer", api.AnswerPromptParams{ID: prompts[0].ID, Value: "yes"}, nil); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("host-key prompt missing")
		case <-time.After(20 * time.Millisecond):
		}
	}
	var view api.TunnelView
	if err = c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = c.Call(ctx, "tunnel.get", api.IDParams{ID: "t"}, &view); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if got, err := echoThrough(view.Endpoints[0].Listen); err != nil || got != "ping" {
		t.Fatalf("forward after client exit: %q %v", got, err)
	}
	c, err = client.Dial(ctx, socket, api.ClientInfo{Kind: "cli", Name: "ssh-reconnect", Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		done <- nil
	case <-ctx.Done():
		t.Fatal("daemon did not exit")
	}
	if _, err := echoThrough(view.Endpoints[0].Listen); err == nil {
		t.Fatal("SSH forward survived shutdown")
	}
}
