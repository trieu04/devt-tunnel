package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Settings struct {
	Tools        map[string]string `toml:"tools,omitempty"`
	Shortcut     string            `toml:"shortcut,omitempty"`
	Shell        string            `toml:"shell,omitempty"`
	CheckUpdates *bool             `toml:"check_updates,omitempty"`
}

func Load(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	meta, err := toml.Decode(string(data), &s)
	if err != nil {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return Settings{}, fmt.Errorf("parse %s: unknown key %q", path, undecoded[0])
	}
	return s, nil
}

func Save(path string, s Settings) error {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
