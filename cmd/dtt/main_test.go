package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestMain(m *testing.M) {
	// Only the compiled test binary can replace the OS credential store.
	if os.Getenv("DTT_PROCESS_TEST") == "1" {
		secretStore = func() secret.Store { return &secret.Memory{} }
		main()
		return
	}
	os.Exit(m.Run())
}

func TestReadStartsDaemonOnDemand(t *testing.T) {
	root := t.TempDir()
	runDir, err := os.MkdirTemp("", "dtt-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	socket := filepath.Join(runDir, "ctl.sock")
	for key, value := range map[string]string{
		"DTT_SOCKET": socket, "DTT_PROCESS_TEST": "1",
		"DEVT_TUNNEL_CONFIG_DIR": filepath.Join(root, "config"),
		"DEVT_TUNNEL_STATE_DIR":  filepath.Join(root, "state"),
	} {
		t.Setenv(key, value)
	}
	cfg := model.Config{Tunnels: []model.Tunnel{{
		ID: "db", Name: "Database", Type: model.TypeSSH,
		SSH:      &model.SSH{Hostname: "127.0.0.1", Flags: []string{"-N"}},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:15432", Target: "127.0.0.1:5432"}},
	}}}
	if err := config.Save(filepath.Join(root, "config", "tunnels.toml"), cfg); err != nil {
		t.Fatal(err)
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) (int, string, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		var out, diagnostics bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostics
		err := cmd.Run()
		if err == nil {
			return 0, out.String(), diagnostics.String()
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), out.String(), diagnostics.String()
		}
		t.Fatalf("invoke %v: %v", args, err)
		return 1, "", ""
	}
	if code, out, diagnostics := invoke("version", "--json"); code != 0 || !strings.Contains(out, `"apiVersion":1`) || diagnostics != "" {
		t.Fatalf("version: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("version started a daemon: %v", err)
	}
	if code, out, diagnostics := invoke("ls", "--no-daemon-start", "--json"); code != 7 || out != "" || !strings.Contains(diagnostics, `"code":"daemon"`) {
		t.Fatalf("no start: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	t.Cleanup(func() {
		if code, out, diagnostics := invoke("daemon", "stop", "--json"); code != 0 {
			t.Errorf("cleanup: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
	})
	code, out, diagnostics := invoke("ls", "--json", "--timeout", "5s")
	var list struct {
		APIVersion int `json:"apiVersion"`
		Tunnels    []struct{ ID, Name, Type, State string }
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil || code != 0 || len(list.Tunnels) != 1 || list.Tunnels[0].ID != "db" || list.APIVersion != 1 {
		t.Fatalf("ls: code=%d stdout=%s stderr=%s decode=%v", code, out, diagnostics, err)
	}
	for _, args := range [][]string{{"addr", "Database", "--port"}, {"show", "d", "--json"}, {"logs", "db", "-n", "1"}, {"daemon", "status", "--json"}, {"daemon", "restart", "--json"}} {
		if code, out, diagnostics := invoke(args...); code != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}

func TestVersionAndUsage(t *testing.T) {
	t.Setenv("DTT_SOCKET", t.TempDir()+"/absent.sock")
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{{[]string{"version"}, 0, "API 1"}, {nil, 0, "Usage:"}, {[]string{"daemon", "bad"}, 2, "unknown"}, {[]string{"daemon", "run", "--idle-exit", "-1s"}, 2, "invalid"}, {[]string{"ls", "--timeout", "0"}, 2, "positive"}} {
		var out, err bytes.Buffer
		if got := run(tc.args, &out, &err); got != tc.code || !strings.Contains(out.String()+err.String(), tc.contains) {
			t.Fatalf("%v: %d %s %s", tc.args, got, &out, &err)
		}
	}
}
