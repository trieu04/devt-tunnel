package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// ToolsDir returns the managed directory for binaries devt-tunnel installs itself.
// Layout: <ToolsDir>/<tool>/<version>/<executable>.
func ToolsDir() (string, error) {
	if override := os.Getenv("DEVT_TUNNEL_DATA_DIR"); override != "" {
		return filepath.Join(override, "tools"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return toolsDir(runtime.GOOS, home, os.Getenv), nil
}

func toolsDir(goos, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		return filepath.Join(fallback(getenv("LOCALAPPDATA"), filepath.Join(home, "AppData", "Local")), appDir, "tools")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDir, "tools")
	}
	return filepath.Join(fallback(getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share")), appDir, "tools")
}

// ExecutableName is tool's file name on this OS (cloudflared.exe on Windows).
func ExecutableName(tool string) string { return exeName(tool) }
