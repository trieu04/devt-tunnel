package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestConfigPathAndReloadRealDaemon(t *testing.T) {
	path := organizeDaemon(t, nil, nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file already exists: %v", err)
	}
	out := organizeSuccess(t, "", "config", "path")
	var result struct{ Path string }
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Path != path {
		t.Fatalf("path: %s (%v)", out, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	var plain, diagnostics bytes.Buffer
	code := Run(context.Background(), []string{"--no-daemon-start", "config", "path"}, IO{In: strings.NewReader(""), Out: &plain, Err: &diagnostics}, Options{})
	if code != 0 || plain.String() != path+"\n" || diagnostics.Len() != 0 {
		t.Fatalf("plain path: code=%d stdout=%s stderr=%s", code, &plain, &diagnostics)
	}
	organizeSuccess(t, "", "config", "reload")
	if err := os.WriteFile(path, []byte("[[profile]]\nid = 'work'\nname = 'Work'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	organizeSuccess(t, "", "config", "reload")
	if out := organizeSuccess(t, "", "profile", "ls"); !strings.Contains(out, `"id":"work"`) {
		t.Fatalf("reload did not apply: %s", out)
	}
	if err := os.WriteFile(path, []byte("not valid = [toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := organizeInvoke("", "config", "reload")
	if code != 1 || out != "" || !strings.Contains(stderr, `"code":"internal"`) || !strings.Contains(stderr, "config ") {
		t.Fatalf("invalid reload: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
	if out := organizeSuccess(t, "", "profile", "ls"); !strings.Contains(out, `"id":"work"`) {
		t.Fatalf("last good config lost: %s", out)
	}
}

func TestConfigEditRealDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("editor fixture uses a POSIX shell script")
	}
	path := organizeDaemon(t, &model.Config{}, nil)
	script := filepath.Join(t.TempDir(), "editor with spaces")
	body := "#!/bin/sh\n[ \"$1\" = '--checked' ] || exit 9\ncat > \"$2\" <<'CONFIG'\n[[profile]]\nid = 'edited'\nname = 'Edited'\nCONFIG\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "'"+script+"' --checked")
	t.Setenv("EDITOR", "/does/not/exist")
	out := organizeSuccess(t, "", "config", "edit")
	if !strings.Contains(out, "edited and reloaded") || !strings.Contains(out, path) {
		t.Fatalf("edit result: %s", out)
	}
	if out := organizeSuccess(t, "", "profile", "ls"); !strings.Contains(out, `"id":"edited"`) {
		t.Fatalf("edit not loaded: %s", out)
	}
	// An explicit editor can be used in scripts, and EDITOR is the fallback.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "'"+script+"' --checked")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'invalid = [' > \"$2\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostics := organizeInvoke("", "config", "edit")
	if code != 1 || out != "" || !strings.Contains(diagnostics, "config ") {
		t.Fatalf("invalid edit: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	if out := organizeSuccess(t, "", "profile", "ls"); !strings.Contains(out, `"id":"edited"`) {
		t.Fatalf("last good config lost: %s", out)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostics = organizeInvoke("", "config", "edit")
	if code != 1 || out != "" || !strings.Contains(diagnostics, "config editor") {
		t.Fatalf("failed editor: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	t.Setenv("EDITOR", "")
	for _, args := range [][]string{{"config", "edit"}, {"config", "edit", "--no-input"}} {
		code, out, diagnostics := organizeInvoke("", args...)
		if code != 4 || out != "" || !strings.Contains(diagnostics, `"code":"needs_user"`) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}

func TestConfigReloadReportsOverviewErrorAndConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "overview", true: "conflict"}[conflict], func(t *testing.T) {
			fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
				switch method {
				case "config.reload":
					if conflict {
						return nil, api.NewError(api.Conflict, "changed on disk")
					}
					return api.Empty{}, nil
				case "config.overview":
					return api.Overview{ConfigPath: "/test/tunnels.toml", ConfigError: "bad configuration"}, nil
				default:
					return nil, api.NewError(api.Internal, "unexpected method")
				}
			})
			code, out, diagnostics := organizeInvoke("", "config", "reload")
			want := 1
			if conflict {
				want = 5
			}
			if code != want || out != "" || !strings.Contains(diagnostics, "bad configuration") {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
			}
		})
	}
}
