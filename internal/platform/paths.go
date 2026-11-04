// Package platform holds everything that differs by operating system.
package platform

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/trieu04/devt-tunnel/internal/core/command"
)

func Home() string {
	home, _ := os.UserHomeDir()
	return home
}

func Dialect() command.Dialect {
	if runtime.GOOS == "windows" {
		return command.Windows
	}
	return command.POSIX
}

const appDir = "devt-tunnel"

// ConfigFile returns the path of tunnels.toml.
func ConfigFile() (string, error) {
	if override := os.Getenv("DEVT_TUNNEL_CONFIG_DIR"); override != "" {
		return filepath.Join(override, "tunnels.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir(runtime.GOOS, home, os.Getenv), "tunnels.toml"), nil
}

// SettingsFile returns settings.toml next to tunnels.toml.
func SettingsFile() (string, error) {
	path, err := ConfigFile()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "settings.toml"), nil
}

// StateDir returns the directory for runtime state.
func StateDir() (string, error) {
	if override := os.Getenv("DEVT_TUNNEL_STATE_DIR"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return stateDir(runtime.GOOS, home, os.Getenv), nil
}

func configDir(goos, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		return filepath.Join(fallback(getenv("APPDATA"), filepath.Join(home, "AppData", "Roaming")), appDir)
	case "darwin":
		// ~/.config rather than Library, so the file can stay with dotfiles.
		return filepath.Join(home, ".config", appDir)
	}
	return filepath.Join(fallback(getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config")), appDir)
}

func stateDir(goos, home string, getenv func(string) string) string {
	switch goos {
	case "windows":
		return filepath.Join(fallback(getenv("LOCALAPPDATA"), filepath.Join(home, "AppData", "Local")), appDir)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDir)
	}
	return filepath.Join(fallback(getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state")), appDir)
}

func fallback(value, def string) string {
	if value != "" {
		return value
	}
	return def
}
