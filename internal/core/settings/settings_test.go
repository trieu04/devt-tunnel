package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.toml")
	want := Settings{Tools: map[string]string{"ssh": "/usr/bin/ssh", "ngrok": "/opt/ngrok"}, Shortcut: "Ctrl+Shift+K", Shell: "powershell"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v err=%v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info, err)
	}
}

func TestLoadMissingAndUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	if got, err := Load(path); err != nil || got.Tools != nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if err := os.WriteFile(path, []byte("shll = 'bash'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("got %v", err)
	}
}
