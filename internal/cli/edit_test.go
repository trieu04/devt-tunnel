package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEditWithExplicitFakeEditor(t *testing.T) {
	defineDaemon(t)
	defineReadResult(t, "add", "command", "Original", "--line", "printf old")
	root := t.TempDir()
	script := filepath.Join(root, "fake editor.sh")
	pathLog := filepath.Join(root, "edited-path")
	t.Setenv("DTT_EDITOR_PATH_LOG", pathLog)
	content := `#!/bin/sh
printf '%s' "$1" > "$DTT_EDITOR_PATH_LOG"
cat > "$1" <<'TOML'
id = "original"
name = "Edited name"
type = "command"
favorite = true
reconnect = true
[command]
line = "printf edited"
ready_log = "edited"
TOML
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", defineJoinArgs([]string{"/bin/sh", script}))
	result := defineReadResult(t, "edit", "Original", "--no-input")
	if result.ID != "original" || !result.Saved {
		t.Fatalf("edit result: %+v", result)
	}
	saved := defineSavedTunnel(t, "Edited name")
	if saved.ID != "original" || !saved.Favorite || !saved.Reconnect || saved.Command.Line != "printf edited" || saved.Command.ReadyLog != "edited" {
		t.Fatalf("editor round trip: %+v command=%+v", saved, saved.Command)
	}
	path, err := os.ReadFile(pathLog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(path)); !os.IsNotExist(err) {
		t.Fatalf("temporary edit file leaked: %s: %v", path, err)
	}
}

func TestEditRejectsInvalidFragmentsWithoutChangingSavedTunnel(t *testing.T) {
	defineDaemon(t)
	defineReadResult(t, "add", "command", "Original", "--line", "printf old")
	t.Setenv("VISUAL", "")
	for _, tc := range []struct {
		name, fragment, message string
		code                    int
	}{
		{"id-change", "id='different'\nname='New'\ntype='command'\n[command]\nline='true'\n", "id must remain", 2},
		{"missing-id", "name='New'\ntype='command'\n[command]\nline='true'\n", "id must remain", 2},
		{"malformed", "id = [broken", "invalid tunnel TOML", 2},
		{"unknown", "id='original'\nname='New'\ntype='command'\nunknown=true\n[command]\nline='true'\n", "unknown tunnel TOML key", 2},
		{"invalid-ready", "id='original'\nname='New'\ntype='command'\n[command]\nline='true'\nready_log='['\n", "cannot build tunnel", 2},
		{"empty-name", "id='original'\nname=''\ntype='command'\n[command]\nline='true'\n", "name must not be empty", 2},
		{"missing-secret", "id='original'\nname='New'\ntype='command'\n[command]\nline='true'\n[command.secret_envs]\nAUTH='missing'\n", "tunnel checks failed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "editor.sh")
			if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > \"$1\" <<'TOML'\n"+tc.fragment+"TOML\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("EDITOR", script)
			code, out, diagnostics := defineRun("edit", "Original", "--json")
			if code != tc.code || !strings.Contains(out+diagnostics, tc.message) || !strings.Contains(diagnostics, `"apiVersion":1`) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
			}
			saved := defineSavedTunnel(t, "Original")
			if saved.ID != "original" || saved.Command.Line != "printf old" {
				t.Fatalf("invalid fragment saved: %+v", saved)
			}
		})
	}
}

func TestEditNeedsTTYOrExplicitEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if code, out, diagnostics := defineRun("edit", "db", "--json"); code != 2 || out != "" || !strings.Contains(diagnostics, "requires a TTY") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	t.Setenv("EDITOR", "'unterminated")
	if code, out, diagnostics := defineRun("edit", "db", "--json"); code != 2 || out != "" || !strings.Contains(diagnostics, "invalid VISUAL or EDITOR") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
}

func TestDefineEditorPreference(t *testing.T) {
	t.Setenv("EDITOR", "editor --wait")
	t.Setenv("VISUAL", "visual --wait")
	if editor, explicit := defineEditor(); editor != "visual --wait" || !explicit {
		t.Fatalf("%s %v", editor, explicit)
	}
	t.Setenv("VISUAL", " ")
	if editor, explicit := defineEditor(); editor != "editor --wait" || !explicit {
		t.Fatalf("%s %v", editor, explicit)
	}
	t.Setenv("EDITOR", "")
	want := "vi"
	if runtime.GOOS == "windows" {
		want = "notepad"
	}
	if editor, explicit := defineEditor(); editor != want || explicit {
		t.Fatalf("%s %v", editor, explicit)
	}
}
