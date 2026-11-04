package state

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Bounds struct{ X, Y, Width, Height int }

type State struct {
	ActiveProfile string               `json:"activeProfile,omitempty"`
	LastUsed      map[string]time.Time `json:"lastUsed,omitempty"`
	Windows       map[string]Bounds    `json:"windows,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}
	}
	if err != nil {
		log.Printf("state: read %s: %v", s.path, err)
		return State{}
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("state: parse %s: %v", s.path, err)
		return State{}
	}
	return st
}

func (s *Store) Update(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st State
	data, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &st); err != nil {
			log.Printf("state: parse %s: %v", s.path, err)
			st = State{}
		}
	}
	fn(&st)
	data, err = json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
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
