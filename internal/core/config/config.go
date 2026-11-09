// Package config loads and saves tunnels.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// Load reads and validates the file. A missing file is an empty config.
func Load(path string) (model.Config, error) {
	var cfg model.Config
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return decode(path, data)
}

func decode(path string, data []byte) (model.Config, error) {
	var cfg model.Config
	meta, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return model.Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, key := range meta.Undecoded() {
		if !legacyKeys[key.String()] {
			return model.Config{}, fmt.Errorf("parse %s: unknown key %q", path, key)
		}
	}
	if err := upgradeLegacy(data, &cfg); err != nil {
		return model.Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := ValidateSaved(cfg); err != nil {
		return model.Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// legacyKeys are accepted from files written before a tunnel had a single
// profile and activation moved out of profiles. The next save drops them.
var legacyKeys = map[string]bool{
	"tunnel.profiles":          true,
	"profile.on_activate":      true,
	"profile.stop_non_members": true,
	"profile.autostart":        true,
}

// upgradeLegacy gives each tunnel the first entry of its old profiles list.
func upgradeLegacy(data []byte, cfg *model.Config) error {
	var legacy struct {
		Tunnels []struct {
			Profiles []string `toml:"profiles"`
		} `toml:"tunnel"`
	}
	if _, err := toml.Decode(string(data), &legacy); err != nil {
		return err
	}
	for n := range cfg.Tunnels {
		if cfg.Tunnels[n].Profile == "" && len(legacy.Tunnels[n].Profiles) > 0 {
			cfg.Tunnels[n].Profile = legacy.Tunnels[n].Profiles[0]
		}
	}
	return nil
}

// Save validates and writes the file atomically, so a crash cannot truncate it.
func Save(path string, cfg model.Config) error {
	if err := ValidateSaved(cfg); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# Written by devt-tunnel. Hand edits are kept; comments are not.\n")
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tunnels-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ValidateSaved reserves tmp- ids and unresolved port requests for runtime.json.
func ValidateSaved(cfg model.Config) error {
	for _, t := range cfg.Tunnels {
		if strings.HasPrefix(t.ID, "tmp-") {
			return fmt.Errorf("saved tunnel id %q uses reserved tmp- prefix", t.ID)
		}
		for _, f := range t.Forwards {
			if f.ListenPort != "" {
				return fmt.Errorf("listen_port is only accepted for ephemeral tunnels")
			}
		}
	}
	return cfg.Validate()
}
