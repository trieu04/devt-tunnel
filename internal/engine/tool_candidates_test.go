package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func fakeCloudflared(t *testing.T, dir, version, counter string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cloudflared")
	script := "#!/bin/sh\necho run >> '" + counter + "'\necho 'cloudflared version " + version + "'\necho second line\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestToolCandidatesAutoSelectedAndProbeCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	root := t.TempDir()
	counter := filepath.Join(root, "runs")
	first := fakeCloudflared(t, filepath.Join(root, "a"), "1", counter)
	second := fakeCloudflared(t, filepath.Join(root, "b"), "2", counter)
	t.Setenv("PATH", filepath.Join(root, "a")+string(os.PathListSeparator)+filepath.Join(root, "b"))
	t.Setenv("DEVT_TUNNEL_DATA_DIR", filepath.Join(root, "data"))
	e, _, _ := newTestEngine(t, model.Config{})

	got, err := e.ToolCandidates("cloudflared")
	if err != nil {
		t.Fatal(err)
	}
	// A machine's own cloudflared may precede ours; Auto is whichever comes first.
	if !got[0].Auto {
		t.Errorf("first candidate should be Auto: %+v", got[0])
	}
	for _, c := range got[1:] {
		if c.Auto {
			t.Errorf("only one Auto expected: %+v", c)
		}
	}
	var mine []string
	for _, c := range got {
		if strings.HasPrefix(c.Path, root) {
			mine = append(mine, c.Path)
			if c.Selected {
				t.Errorf("flags wrong: %+v", c)
			}
			if !strings.HasPrefix(c.Version, "cloudflared version ") || strings.Contains(c.Version, "second") {
				t.Errorf("version = %q", c.Version)
			}
		}
	}
	if len(mine) != 2 || mine[0] != first || mine[1] != second {
		t.Fatalf("candidates = %v", mine)
	}
	if _, err := e.SetToolPath("cloudflared", second); err != nil {
		t.Fatal(err)
	}
	got, _ = e.ToolCandidates("cloudflared")
	for _, c := range got {
		if c.Selected != (c.Path == second) {
			t.Errorf("selected wrong: %+v", c)
		}
	}
	runs, _ := os.ReadFile(counter)
	if n := strings.Count(string(runs), "run"); n != 2 {
		t.Fatalf("each binary should run once, ran %d times", n)
	}
	info := e.Tools()
	if info[2].Origin != "path" && info[2].Origin != "custom" {
		t.Errorf("origin = %q", info[2].Origin)
	}
}

func TestToolCandidatesRejectsUnknownTool(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	if _, err := e.ToolCandidates("rm"); err == nil {
		t.Fatal("unsupported tool accepted")
	}
}
