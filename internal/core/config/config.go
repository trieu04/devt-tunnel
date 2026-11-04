// Package config loads and saves tunnels.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return model.Config{}, fmt.Errorf("parse %s: unknown key %q", path, undecoded[0])
	}
	if err := cfg.Validate(); err != nil {
		return model.Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Save validates and writes the file atomically, so a crash cannot truncate it.
func Save(path string, cfg model.Config) error {
	if err := cfg.Validate(); err != nil {
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
