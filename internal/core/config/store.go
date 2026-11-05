package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

var ErrConflict = errors.New("tunnels.toml changed on disk")

// Store serializes edits and detects changes made outside the application.
type Store struct {
	path   string
	mu     sync.Mutex
	seen   [sha256.Size]byte
	loaded bool
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (model.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, data, err := s.read()
	if err != nil {
		return model.Config{}, err
	}
	s.remember(data)
	return cfg, nil
}

func (s *Store) Changed() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return false, err
	}
	if !s.loaded {
		return false, nil
	}
	return sha256.Sum256(data) != s.seen, nil
}

func (s *Store) Update(fn func(*model.Config) error) (model.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return model.Config{}, err
	}
	if s.loaded && sha256.Sum256(data) != s.seen {
		return model.Config{}, ErrConflict
	}
	var cfg model.Config
	if len(data) != 0 {
		cfg, err = decode(s.path, data)
		if err != nil {
			return model.Config{}, err
		}
	}
	if err := fn(&cfg); err != nil {
		return model.Config{}, err
	}
	if err := ValidateSaved(cfg); err != nil {
		return model.Config{}, err
	}
	if len(data) != 0 {
		if err := os.WriteFile(s.path+".bak", data, 0o600); err != nil {
			return model.Config{}, fmt.Errorf("backup config: %w", err)
		}
	}
	if err := Save(s.path, cfg); err != nil {
		return model.Config{}, err
	}
	newData, err := os.ReadFile(s.path)
	if err != nil {
		return model.Config{}, err
	}
	s.remember(newData)
	return cfg, nil
}

func (s *Store) read() (model.Config, []byte, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return model.Config{}, nil, nil
	}
	if err != nil {
		return model.Config{}, nil, err
	}
	cfg, err := decode(s.path, data)
	return cfg, data, err
}

func (s *Store) remember(data []byte) { s.seen = sha256.Sum256(data); s.loaded = true }
