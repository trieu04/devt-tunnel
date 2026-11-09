package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestCompletionScripts(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			if code := Run(context.Background(), []string{"completion", shell}, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 || out.Len() < 100 || !strings.Contains(out.String(), "devt-tunnel") || diagnostics.Len() != 0 {
				t.Fatalf("code=%d bytes=%d stderr=%s", code, out.Len(), &diagnostics)
			}
		})
	}
	for _, args := range [][]string{{"completion"}, {"completion", "bad"}, {"completion", "bash", "extra"}} {
		if code := Run(context.Background(), args, IO{}, Options{}); code != 2 {
			t.Errorf("%v: code=%d, want 2", args, code)
		}
	}
}

func TestDynamicCompletions(t *testing.T) {
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method == "group.list" {
			return []model.Group{{ID: "backend", Name: "Backend"}}, nil
		}
		if method == "profile.list" {
			return []model.Profile{{ID: "work", Name: "Work projects"}, {ID: "home", Name: "home"}}, nil
		}
		return readFixture(method, params)
	})
	for _, tc := range []struct {
		path   []string
		args   []string
		prefix string
		want   []string
	}{
		{[]string{"show"}, nil, "db", []string{"db-prod", "db-test"}},
		{[]string{"show"}, nil, "Prod", []string{"Production DB"}},
		{[]string{"show"}, []string{"db-prod"}, "", nil},
		{[]string{"start"}, []string{"db-prod"}, "db-t", []string{"db-test"}},
		{[]string{"profile", "show"}, nil, "W", []string{"Work projects"}},
		{[]string{"profile", "rm"}, nil, "wo", []string{"work"}},
		{[]string{"group", "start"}, nil, "back", []string{"backend"}},
		{[]string{"group", "assign"}, nil, "Back", []string{"Backend"}},
		{[]string{"group", "assign"}, []string{"backend"}, "db", []string{"db-prod", "db-test"}},
		{[]string{"set"}, []string{"db-prod"}, "", nil},
	} {
		a := newApp(IO{}, Options{})
		root := newRoot(a)
		cmd, _, err := root.Find(tc.path)
		if err != nil || cmd.ValidArgsFunction == nil {
			t.Fatalf("%v: missing completion, err=%v", tc.path, err)
		}
		got, directive := cmd.ValidArgsFunction(cmd, tc.args, tc.prefix)
		if !reflect.DeepEqual(got, tc.want) || directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%v %v %q: %v directive=%v, want %v", tc.path, tc.args, tc.prefix, got, directive, tc.want)
		}
		if a.noDaemonStart || a.conn != nil {
			t.Fatal("completion mutated the executing app")
		}
	}
	for _, path := range [][]string{{"ls"}, {"forward"}, {"add", "ssh"}, {"hosts"}} {
		root := newRoot(newApp(IO{}, Options{}))
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		complete, ok := cmd.GetFlagCompletionFunc("profile")
		if !ok {
			t.Fatalf("%v lacks --profile completion", path)
		}
		got, _ := complete(cmd, nil, "wo")
		if !reflect.DeepEqual(got, []string{"work"}) {
			t.Errorf("%v --profile: %v", path, got)
		}
		got, _ = complete(cmd, nil, "Work")
		if path[0] == "ls" || path[0] == "hosts" {
			if len(got) != 0 {
				t.Errorf("%v --profile suggested names for an ID-only flag: %v", path, got)
			}
		} else if !reflect.DeepEqual(got, []string{"Work projects"}) {
			t.Errorf("%v --profile missing profile name: %v", path, got)
		}
	}
	// Exercise Cobra's hidden request command, not only the callbacks directly.
	var out, diagnostics bytes.Buffer
	if code := Run(context.Background(), []string{"__complete", "show", "db"}, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 || out.String() != "db-prod\ndb-test\n:4\n" {
		t.Fatalf("completion protocol: code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
	}
}

func TestCompletionReferencesCovered(t *testing.T) {
	root := newRoot(newApp(IO{}, Options{}))
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if strings.Contains(cmd.Use, "<t>") || strings.Contains(cmd.Use, "[t]") || strings.Contains(cmd.Use, "<p>") {
			if cmd.ValidArgsFunction == nil {
				t.Errorf("%s has a reference argument without completion", cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestCompletionUnavailableIsSilentAndDoesNotStartDaemon(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "absent.sock")
	t.Setenv("DTT_SOCKET", socket)
	var out, diagnostics bytes.Buffer
	a := newApp(IO{Out: &out, Err: &diagnostics}, Options{})
	root := newRoot(a)
	cmd, _, _ := root.Find([]string{"show"})
	start := time.Now()
	got, directive := cmd.ValidArgsFunction(cmd, nil, "")
	if len(got) != 0 || directive != cobra.ShellCompDirectiveNoFileComp || out.Len() != 0 || diagnostics.Len() != 0 {
		t.Fatalf("got=%v directive=%v stdout=%s stderr=%s", got, directive, &out, &diagnostics)
	}
	if time.Since(start) > time.Second {
		t.Fatal("unavailable completion did not return quickly")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("completion created a socket: %v", err)
	}
}

func TestCompletionSlowDaemonIsBounded(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		<-release
		return nil, nil
	})
	root := newRoot(newApp(IO{}, Options{}))
	cmd, _, _ := root.Find([]string{"show"})
	start := time.Now()
	got, _ := cmd.ValidArgsFunction(cmd, nil, "")
	if len(got) != 0 || time.Since(start) > time.Second {
		t.Fatalf("slow completion got=%v elapsed=%s", got, time.Since(start))
	}
}
