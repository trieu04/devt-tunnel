package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// bundleVarPrefixes are set by AppImage launchers for the app's own libraries.
// Host binaries must not inherit them, or they may load the wrong libraries.
var bundleVarPrefixes = []string{
	"LD_LIBRARY_PATH=", "LD_PRELOAD=", "GTK_", "GIO_", "GDK_", "GSETTINGS_SCHEMA_DIR=", "APPDIR=", "APPIMAGE=", "ARGV0=", "OWD=",
}

// CleanEnviron removes bundle variables when running from an AppImage.
func CleanEnviron(environ []string) []string {
	bundled := slices.ContainsFunc(environ, func(e string) bool { return strings.HasPrefix(e, "APPDIR=") })
	if !bundled {
		return slices.Clone(environ)
	}
	return slices.DeleteFunc(slices.Clone(environ), func(e string) bool {
		return slices.ContainsFunc(bundleVarPrefixes, func(p string) bool { return strings.HasPrefix(e, p) })
	})
}

// Tools locates host binaries. Apps opened from Finder or a launcher do not inherit the
// terminal's PATH, so a plain lookup would report Homebrew's ngrok as missing.
type Tools struct {
	Overrides map[string]string // guarded by mu

	mu   sync.RWMutex
	once sync.Once
	path string
}

// PATH returns the search path children should run with.
func (t *Tools) PATH() string {
	t.once.Do(func() { t.path = mergePath(loginShellPATH(), knownDirs(), os.Getenv("PATH")) })
	return t.path
}

// Environ returns the environment for child processes.
func (t *Tools) Environ() []string {
	env := slices.DeleteFunc(CleanEnviron(os.Environ()), func(e string) bool {
		return strings.HasPrefix(strings.ToUpper(e), "PATH=")
	})
	return append(env, "PATH="+t.PATH())
}

// Resolve returns the absolute path of a tool.
func (t *Tools) Resolve(tool string) (string, error) {
	p, _, err := t.Locate(tool)
	return p, err
}

// Locate returns a tool path and the source used to find it.
func (t *Tools) Locate(tool string) (string, string, error) {
	t.mu.RLock()
	override := t.Overrides[tool]
	t.mu.RUnlock()
	if override != "" {
		return override, "settings", nil
	}
	if filepath.IsAbs(tool) {
		return tool, "absolute path", nil
	}
	for _, dir := range filepath.SplitList(t.PATH()) {
		candidate := filepath.Join(dir, tool)
		if p, err := exec.LookPath(candidate); err == nil {
			return p, "search path", nil
		}
	}
	return "", "", fmt.Errorf("%s was not found on this machine", tool)
}

func (t *Tools) SetOverrides(overrides map[string]string) {
	t.mu.Lock()
	t.Overrides = make(map[string]string, len(overrides))
	for name, path := range overrides {
		t.Overrides[name] = path
	}
	t.mu.Unlock()
}

func (t *Tools) Override(tool string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Overrides[tool]
}

func mergePath(parts ...any) string {
	var dirs []string
	add := func(s string) {
		for _, d := range filepath.SplitList(s) {
			if d != "" && !slices.Contains(dirs, d) {
				dirs = append(dirs, d)
			}
		}
	}
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			add(v)
		case []string:
			for _, s := range v {
				add(s)
			}
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func knownDirs() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH"),
			filepath.Join(os.Getenv("ProgramFiles"), "cloudflared"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links"),
			filepath.Join(home, "scoop", "shims"),
		}
	}
	return []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), "/usr/bin", "/bin", "/snap/bin"}
}

var pathMarker = regexp.MustCompile(`__DTT__(.*)__DTT__`)

// loginShellPATH asks the user's shell. Startup files may print anything, hence the markers.
func loginShellPATH() string {
	shell := os.Getenv("SHELL")
	if runtime.GOOS == "windows" || shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", `printf '__DTT__%s__DTT__' "$PATH"`).Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	if m := pathMarker.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// Shell returns the prefix that runs a command tunnel's line.
func Shell() []string {
	return ShellFor("")
}

func ShellFor(setting string) []string {
	if runtime.GOOS == "windows" {
		if strings.EqualFold(setting, "powershell") || strings.EqualFold(setting, "pwsh") {
			return []string{"powershell.exe", "-NoProfile", "-Command"}
		}
		return []string{"cmd.exe", "/d", "/s", "/c"}
	}
	if setting != "" {
		return []string{setting, "-c"}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-c"}
}
