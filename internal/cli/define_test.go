package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/control"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

// Use the real daemon control server and engine, not RPC fixtures, so persistence,
// editor defaults, importer parsing and command previews are exercised together.
func defineDaemon(t *testing.T, prepare ...func(*engine.Options)) *engine.Engine {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	root := t.TempDir()
	runDir, err := os.MkdirTemp("", "dtt-define-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	if err := platform.PreparePrivateDir(runDir); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"DTT_SOCKET":             filepath.Join(runDir, "ctl.sock"),
		"DEVT_TUNNEL_CONFIG_DIR": filepath.Join(root, "config"),
		"DEVT_TUNNEL_STATE_DIR":  filepath.Join(root, "state"),
		"HOME":                   root, "USERPROFILE": root, "SHELL": "/bin/sh",
	} {
		t.Setenv(key, value)
	}
	path, err := platform.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(path, model.Config{Profiles: []model.Profile{{ID: "work", Name: "Work"}}}); err != nil {
		t.Fatal(err)
	}
	settingsPath, err := platform.SettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(settingsPath, settings.Settings{Shell: "/bin/sh", Tools: map[string]string{"ssh": truePath, "ngrok": truePath, "cloudflared": truePath}}); err != nil {
		t.Fatal(err)
	}
	store := &secret.Memory{}
	if err := store.Set("test-auth", "never-print-this-value"); err != nil {
		t.Fatal(err)
	}
	opts := engine.Options{Secrets: store, AskpassHelper: "unused"}
	for _, f := range prepare {
		f(&opts)
	}
	e, err := engine.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	listener, err := net.Listen("unix", os.Getenv("DTT_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	server := control.New(e, "test", nil)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = listener.Close()
		server.Close()
		select {
		case err := <-served:
			if err != nil && err != net.ErrClosed {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("control server did not stop")
		}
	})
	return e
}

func defineRun(args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	args = append([]string{"--no-daemon-start"}, args...)
	code := Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostics}, Options{Version: "test"})
	return code, out.String(), diagnostics.String()
}

type defineResult struct {
	APIVersion             int              `json:"apiVersion"`
	ID                     string           `json:"id"`
	Preview                string           `json:"preview"`
	BuildError             string           `json:"buildError"`
	Findings               []checks.Finding `json:"findings"`
	DryRun, Saved, Started bool
}

func defineReadResult(t *testing.T, args ...string) defineResult {
	t.Helper()
	args = append(args, "--json")
	code, out, diagnostics := defineRun(args...)
	var result defineResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || diagnostics != "" || result.APIVersion != api.Version {
		t.Fatalf("%v: code=%d stdout=%s stderr=%s decode=%v", args, code, out, diagnostics, err)
	}
	return result
}

func defineSavedTunnel(t *testing.T, ref string) model.Tunnel {
	t.Helper()
	code, out, diagnostics := defineRun("show", ref, "--json")
	var result struct{ Tunnel model.Tunnel }
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || diagnostics != "" {
		t.Fatalf("show %q: code=%d stdout=%s stderr=%s decode=%v", ref, code, out, diagnostics, err)
	}
	return result.Tunnel
}

func TestDefineSaveSetDuplicateRemoveRoundTrip(t *testing.T) {
	defineDaemon(t)
	added := defineReadResult(t, "add", "command", "Original", "--line", "printf ready", "--profile", "Work", "--favorite", "--start-on-launch", "--no-reconnect")
	if added.ID != "original" || !added.Saved || added.Started || added.DryRun {
		t.Fatalf("add result: %+v", added)
	}
	saved := defineSavedTunnel(t, "Original")
	if saved.ID != "original" || !saved.Favorite || !saved.StartOnLaunch || saved.Reconnect || len(saved.Profiles) != 1 || saved.Profiles[0] != "work" || saved.Command.Line != "printf ready" {
		t.Fatalf("round trip: %+v", saved)
	}
	changed := defineReadResult(t, "set", "original", "name=Renamed", "favorite=false", "reconnect=true", "start_on_launch=false")
	if changed.ID != "original" || !changed.Saved {
		t.Fatalf("set result: %+v", changed)
	}
	saved = defineSavedTunnel(t, "Renamed")
	if saved.ID != "original" || saved.Favorite || saved.StartOnLaunch || !saved.Reconnect {
		t.Fatalf("set changed ID or failed: %+v", saved)
	}
	duplicate := defineReadResult(t, "dup", "original", "--name", "Copy")
	if duplicate.ID != "copy" || !duplicate.Saved {
		t.Fatalf("dup: %+v", duplicate)
	}
	copy := defineSavedTunnel(t, "copy")
	if copy.Command.Line != saved.Command.Line || len(copy.Profiles) != 1 || copy.Profiles[0] != "work" || copy.Name != "Copy" {
		t.Fatalf("duplicate did not preserve definition: %+v", copy)
	}
	// Resolve all refs before mutation; a later bad ref must leave the first intact.
	if code, _, diagnostics := defineRun("rm", "original", "missing", "--yes"); code != 2 || !strings.Contains(diagnostics, "unknown tunnel") {
		t.Fatalf("rm bad ref: %d %s", code, diagnostics)
	}
	defineSavedTunnel(t, "original")
	code, out, diagnostics := defineRun("rm", "original", "copy", "copy", "--yes", "--json")
	var removed struct {
		APIVersion int `json:"apiVersion"`
		IDs        []string
		Deleted    bool
	}
	if err := json.Unmarshal([]byte(out), &removed); err != nil || code != 0 || diagnostics != "" || !removed.Deleted || strings.Join(removed.IDs, ",") != "original,copy" || removed.APIVersion != api.Version {
		t.Fatalf("rm: code=%d stdout=%s stderr=%s decode=%v", code, out, diagnostics, err)
	}
	if code, _, _ := defineRun("show", "original"); code != 2 {
		t.Fatalf("removed tunnel still exists: %d", code)
	}
}

func TestDefineUsageBeforeConnecting(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	for _, tc := range []struct {
		args []string
		text string
	}{
		{[]string{"set", "db", "unknown=true"}, "valid keys: favorite, name, reconnect, start_on_launch"},
		{[]string{"set", "db", "favorite=maybe"}, "must be a boolean"},
		{[]string{"set", "db", "name="}, "must not be empty"},
		{[]string{"rm", "db"}, "requires --yes without a TTY"},
		{[]string{"rm", "db", "--no-input"}, "requires --yes without a TTY"},
		{[]string{"dup", "db"}, "--name is required"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append(tc.args, "--json")
			code, out, diagnostics := defineRun(args...)
			if code != 2 || out != "" || !strings.Contains(diagnostics, tc.text) || !strings.Contains(diagnostics, `"apiVersion":1`) || !strings.Contains(diagnostics, `"code":"usage"`) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
			}
		})
	}
}

func TestDefineImportQuotesWarningsAndNoCredentials(t *testing.T) {
	defineDaemon(t)
	for _, tc := range []struct {
		name string
		args []string
		line string
	}{
		{"argv", []string{"printf", "%s\\n", "space and 'quote' $HOME ; literal"}, `printf '%s\n' 'space and '\''quote'\'' $HOME ; literal'`},
		{"pasted", []string{`printf '%s' hello | cat`}, `printf '%s' hello | cat`},
	} {
		args := append([]string{"import", "--name", tc.name, "--profile", "work", "--"}, tc.args...)
		// --json must occur before --, where CLI flag parsing stops.
		args = append([]string{"--json"}, args...)
		code, out, diagnostics := defineRun(args...)
		var result defineResult
		if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || diagnostics != "" || !result.Saved {
			t.Fatalf("import: code=%d stdout=%s stderr=%s decode=%v", code, out, diagnostics, err)
		}
		saved := defineSavedTunnel(t, tc.name)
		if saved.Command == nil || saved.Command.Line != tc.line || !saved.Reconnect || strings.Join(saved.Profiles, ",") != "work" {
			t.Fatalf("import round trip: %+v command=%+v want=%s", saved, saved.Command, tc.line)
		}
		if tc.name == "pasted" {
			found := false
			for _, finding := range result.Findings {
				found = found || strings.Contains(finding.Message, "Shell operators")
			}
			if !found {
				t.Fatalf("missing importer warning: %+v", result.Findings)
			}
		}
	}
	for _, args := range [][]string{
		{"import", "--json", "--", "ngrok", "http", "3000", "--authtoken", "VERY-PRIVATE"},
		{"import", "--json", "--", "cloudflared tunnel run --token VERY-PRIVATE"},
	} {
		code, out, diagnostics := defineRun(args...)
		if code != 2 || strings.Contains(out+diagnostics, "VERY-PRIVATE") || !strings.Contains(diagnostics, "inline credentials") {
			t.Fatalf("credential import: code=%d stdout=%s stderr=%s", code, out, diagnostics)
		}
	}
	code, out, diagnostics := defineRun("import", "--dry-run", "--json", "--", "ssh", "-N", "-L", "15432:127.0.0.1:5432", "server1")
	if code != 0 || diagnostics != "" || !strings.Contains(out, "was not found in the known hosts list") || !strings.Contains(out, `"saved":false`) {
		t.Fatalf("SSH import warnings: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
}

func TestDefineStartPassedToSaveAndDryRunWins(t *testing.T) {
	var starts []bool
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.newDraft":
			return model.Tunnel{Type: model.TypeCommand, Reconnect: true, Command: &model.Command{}}, nil
		case "tunnel.evaluateDraft":
			return api.DraftResult{Preview: "exact-daemon-preview", Findings: []checks.Finding{}}, nil
		case "tunnel.save":
			var p api.SaveTunnelParams
			if err := json.Unmarshal(params, &p); err != nil {
				t.Error(err)
			}
			starts = append(starts, p.Start)
			return "created", nil
		default:
			return nil, api.NewError(api.Internal, "unexpected method "+method)
		}
	})
	started := defineReadResult(t, "add", "command", "created", "--line", "true", "--start")
	dry := defineReadResult(t, "add", "command", "created", "--line", "true", "--start", "--dry-run")
	if len(starts) != 1 || !starts[0] || !started.Started || dry.Saved || dry.Started || dry.Preview != "exact-daemon-preview" {
		t.Fatalf("starts=%v started=%+v dry=%+v", starts, started, dry)
	}
}
