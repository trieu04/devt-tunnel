package secret

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Kind string

const (
	KindSSHPassphrase Kind = "ssh_passphrase"
	KindNgrok         Kind = "ngrok"
	KindCloudflared   Kind = "cloudflared"
	KindEnv           Kind = "env"
)

type Entry struct {
	Name     string    `json:"name"`
	Kind     Kind      `json:"kind"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed,omitempty"`
}

type Index struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

func (i *Index) clock() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

func (i *Index) List() []Entry {
	i.mu.Lock()
	defer i.mu.Unlock()
	entries, _ := i.loadLocked()
	return entries
}

func (i *Index) Upsert(name string, kind Kind) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	entries, _ := i.loadLocked()
	for n := range entries {
		if entries[n].Name == name {
			entries[n].Kind = kind
			return i.saveLocked(entries)
		}
	}
	entries = append(entries, Entry{Name: name, Kind: kind, Created: i.clock()})
	return i.saveLocked(entries)
}

func (i *Index) Delete(name string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	entries, _ := i.loadLocked()
	out := entries[:0]
	for _, entry := range entries {
		if entry.Name != name {
			out = append(out, entry)
		}
	}
	return i.saveLocked(out)
}

func (i *Index) Rename(from, to string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	entries, _ := i.loadLocked()
	for n := range entries {
		if entries[n].Name == from {
			entries[n].Name = to
		}
	}
	return i.saveLocked(entries)
}

func (i *Index) MarkUsed(name string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	entries, _ := i.loadLocked()
	for n := range entries {
		if entries[n].Name == name {
			entries[n].LastUsed = i.clock()
			_ = i.saveLocked(entries)
			return
		}
	}
}

func (i *Index) loadLocked() ([]Entry, error) {
	data, err := os.ReadFile(i.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return []Entry{}, err
	}
	return entries, nil
}

func (i *Index) saveLocked(entries []Entry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(i.Path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(i.Path), ".vault-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), i.Path)
}
