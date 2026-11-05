package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/control"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

// Exercise the real engine and control server, not a canned RPC response.
func organizeDaemon(t *testing.T, cfg *model.Config, store secret.Store) string {
	t.Helper()
	root := t.TempDir()
	run, err := os.MkdirTemp("", "dtt-organize-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run) })
	if err := platform.PreparePrivateDir(run); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DTT_SOCKET", filepath.Join(run, "ctl.sock"))
	t.Setenv("DEVT_TUNNEL_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("DEVT_TUNNEL_STATE_DIR", filepath.Join(root, "state"))
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("SHELL", "")
	path, err := platform.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		if err := config.Save(path, *cfg); err != nil {
			t.Fatal(err)
		}
	}
	if store == nil {
		store = &secret.Memory{}
	}
	e, err := engine.New(engine.Options{Secrets: store, AskpassHelper: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	server := control.New(e, "organize-test", nil)
	listener, err := net.Listen("unix", os.Getenv("DTT_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return path
}

func organizeInvoke(input string, args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	args = append([]string{"--no-daemon-start", "--json"}, args...)
	code := Run(context.Background(), args, IO{In: strings.NewReader(input), Out: &out, Err: &diagnostics}, Options{Version: "test"})
	return code, out.String(), diagnostics.String()
}

func organizeSuccess(t *testing.T, input string, args ...string) string {
	t.Helper()
	code, out, diagnostics := organizeInvoke(input, args...)
	var envelope struct {
		APIVersion int `json:"apiVersion"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); code != 0 || diagnostics != "" || err != nil || envelope.APIVersion != api.Version {
		t.Fatalf("%v: code=%d stdout=%s stderr=%s decode=%v", args, code, out, diagnostics, err)
	}
	return out
}

func TestProfileRoundTripRealDaemon(t *testing.T) {
	cfg := model.Config{
		Profiles: []model.Profile{{ID: "ops", Name: "Operations"}},
		Tunnels:  []model.Tunnel{{ID: "db", Name: "Database", Type: model.TypeCommand, Profiles: []string{"ops"}, Command: &model.Command{Line: "echo ready"}}},
	}
	path := organizeDaemon(t, &cfg, nil)
	out := organizeSuccess(t, "", "profile", "add", "Work projects", "--ssh-config", "/test/ssh/config", "--on-activate", "start_autostart", "--stop-non-members")
	var added struct{ Profile model.Profile }
	if err := json.Unmarshal([]byte(out), &added); err != nil || added.Profile.ID == "" || added.Profile.OnActivate != "start_autostart" || !added.Profile.StopNonMembers || added.Profile.SSHConfig != "/test/ssh/config" {
		t.Fatalf("add: %s (%v)", out, err)
	}
	id := added.Profile.ID
	organizeSuccess(t, "", "profile", "use", "Work projects")
	out = organizeSuccess(t, "", "profile", "ls")
	var listed struct {
		ActiveProfile string
		Profiles      []profileView
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || listed.ActiveProfile != id || len(listed.Profiles) != 2 || !listed.Profiles[1].Active {
		t.Fatalf("ls: %s (%v)", out, err)
	}
	organizeSuccess(t, "", "profile", "use", "--none")
	organizeSuccess(t, "", "profile", "use", id[:len(id)-1])
	for _, action := range []string{"start", "stop"} {
		organizeSuccess(t, "", "profile", action, id)
	}
	organizeSuccess(t, "", "profile", "autostart", "Operations", "Database", "on")
	out = organizeSuccess(t, "", "profile", "show", "op")
	var shown struct{ Profile profileView }
	if err := json.Unmarshal([]byte(out), &shown); err != nil || len(shown.Profile.Members) != 1 || !shown.Profile.Members[0].Autostart || shown.Profile.Members[0].ID != "db" {
		t.Fatalf("show: %s (%v)", out, err)
	}
	organizeSuccess(t, "", "profile", "autostart", "ops", "db", "off")
	organizeSuccess(t, "", "profile", "rm", id)
	organizeSuccess(t, "", "profile", "rm", "ops")
	out = organizeSuccess(t, "", "profile", "ls")
	if err := json.Unmarshal([]byte(out), &listed); err != nil || listed.ActiveProfile != "" || len(listed.Profiles) != 0 {
		t.Fatalf("after rm: %s (%v)", out, err)
	}
	saved, err := config.Load(path)
	if err != nil || len(saved.Tunnels) != 1 || saved.Tunnels[0].ID != "db" || len(saved.Tunnels[0].Profiles) != 0 {
		t.Fatalf("tunnels not kept: %+v (%v)", saved, err)
	}
}

func TestProfileReferenceResolution(t *testing.T) {
	profiles := []model.Profile{{ID: "work-main", Name: "Work"}, {ID: "work-test", Name: "Testing"}, {ID: "alias", Name: "work-main"}, {ID: "named", Name: "work"}}
	for _, tc := range []struct{ ref, id, message string }{
		{"work-main", "work-main", ""}, {"Work", "work-main", ""}, {"work-m", "work-main", ""}, {"work", "named", ""},
		{"wo", "", "work-main, work-test"}, {"missing", "", "unknown profile"}, {"", "", "unknown profile"},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			p, err := profileMatch(profiles, tc.ref)
			if tc.message != "" {
				if ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil || p.ID != tc.id {
				t.Fatalf("profile=%+v err=%v", p, err)
			}
		})
	}
	profiles[1].Name = "Work"
	if _, err := profileMatch(profiles, "Work"); ExitCode(err) != 2 || !strings.Contains(err.Error(), "work-main, work-test") {
		t.Fatalf("duplicate names: %v", err)
	}
}

func TestProfileUsage(t *testing.T) {
	organizeDaemon(t, &model.Config{Profiles: []model.Profile{{ID: "work", Name: "Work"}}, Tunnels: []model.Tunnel{{ID: "db", Name: "Database", Type: model.TypeCommand, Command: &model.Command{Line: "echo ready"}}}}, nil)
	for _, args := range [][]string{
		{"profile", "add", " "}, {"profile", "add", "bad", "--on-activate", "invalid"},
		{"profile", "use"}, {"profile", "use", "work", "--none"}, {"profile", "show", "missing"},
		{"profile", "autostart", "work", "db", "maybe"}, {"profile", "autostart", "work", "db", "on"},
	} {
		code, out, diagnostics := organizeInvoke("", args...)
		if code != 2 || out != "" || !strings.Contains(diagnostics, `"code":"usage"`) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}
