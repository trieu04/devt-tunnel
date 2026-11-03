// Package platform holds everything that differs by operating system.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

const appDir = "devt-tunnel"

// ConfigFile returns the path of tunnels.toml.
func ConfigFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir(runtime.GOOS, home, os.Getenv), "tunnels.toml"), nil
}

// StateDir returns the directory for runtime state.
func StateDir() (string, error) {
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
