package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToolOrigin(t *testing.T) {
	cases := []struct{ goos, path, tools, want string }{
		{"darwin", "/opt/homebrew/bin/ngrok", "", OriginHomebrew},
		{"darwin", "/usr/local/Cellar/ngrok/3/bin/ngrok", "", OriginHomebrew},
		{"darwin", "/usr/bin/ssh", "", OriginSystem},
		{"darwin", "/Users/a/Library/Application Support/devt-tunnel/tools/cloudflared/1/cloudflared", "/Users/a/Library/Application Support/devt-tunnel/tools", OriginDownloaded},
		{"darwin", "/usr/local/bin/cloudflared", "", OriginPath},
		{"linux", "/home/linuxbrew/.linuxbrew/bin/ngrok", "", OriginHomebrew},
		{"linux", "/bin/ssh", "", OriginSystem},
		{"linux", "/home/a/.local/share/devt-tunnel/tools/ngrok/1/ngrok", "/home/a/.local/share/devt-tunnel/tools", OriginDownloaded},
		{"linux", "/snap/bin/ngrok", "", OriginPath},
		{"windows", `C:\Users\a\scoop\shims\ngrok.exe`, "", OriginScoop},
		{"windows", `C:\Users\a\AppData\Local\Microsoft\WinGet\Links\ngrok.exe`, "", OriginWinGet},
		{"windows", `C:\Windows\System32\OpenSSH\ssh.exe`, "", OriginSystem},
		{"windows", `c:\users\a\appdata\local\devt-tunnel\tools\cloudflared\1\cloudflared.exe`, `C:\Users\a\AppData\Local\devt-tunnel\tools`, OriginDownloaded},
		{"windows", `C:\Program Files\cloudflared\cloudflared.exe`, "", OriginPath},
	}
	for _, c := range cases {
		if got := ToolOrigin(c.goos, c.path, c.tools); got != c.want {
			t.Errorf("ToolOrigin(%s, %s) = %s, want %s", c.goos, c.path, got, c.want)
		}
	}
}

func TestToolsDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := toolsDir("darwin", "/h", env(nil)); got != filepath.Join("/h", "Library", "Application Support", "devt-tunnel", "tools") {
		t.Error(got)
	}
	if got := toolsDir("linux", "/h", env(nil)); got != filepath.Join("/h", ".local", "share", "devt-tunnel", "tools") {
		t.Error(got)
	}
	if got := toolsDir("linux", "/h", env(map[string]string{"XDG_DATA_HOME": "/x"})); got != filepath.Join("/x", "devt-tunnel", "tools") {
		t.Error(got)
	}
	if got := toolsDir("windows", "/h", env(map[string]string{"LOCALAPPDATA": "/l"})); got != filepath.Join("/l", "devt-tunnel", "tools") {
		t.Error(got)
	}
	t.Setenv("DEVT_TUNNEL_DATA_DIR", "/data")
	if got, _ := ToolsDir(); got != filepath.Join("/data", "tools") {
		t.Error(got)
	}
}

func fakeExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverWalksAllDirsMergesSymlinksAndAddsManagedAndCustom(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	fakeExe(t, filepath.Join(a, exeName("tool")))
	fakeExe(t, filepath.Join(b, exeName("tool")))
	tools := filepath.Join(root, "tools")
	fakeExe(t, filepath.Join(tools, "tool", "1.0", exeName("tool")))
	custom := filepath.Join(root, "elsewhere", exeName("tool"))
	fakeExe(t, custom)
	c := filepath.Join(root, "c")
	if err := os.MkdirAll(c, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(a, "tool"), filepath.Join(c, "tool")); err != nil {
			t.Fatal(err)
		}
	}
	got := discover("tool", []string{a, b, c, "relative", filepath.Join(root, "missing")}, tools, custom)
	want := []struct{ path, origin string }{
		{filepath.Join(a, exeName("tool")), OriginPath},
		{filepath.Join(b, exeName("tool")), OriginPath},
		{filepath.Join(tools, "tool", "1.0", exeName("tool")), OriginDownloaded},
		{custom, OriginCustom},
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Origin != w.origin {
			t.Errorf("[%d] = %#v, want %v", i, got[i], w)
		}
	}
}

func TestDiscoverOverrideInPathIsNotDuplicated(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, exeName("tool"))
	fakeExe(t, p)
	if got := discover("tool", []string{root}, "", p); len(got) != 1 || got[0].Origin != OriginPath {
		t.Fatalf("%#v", got)
	}
}
