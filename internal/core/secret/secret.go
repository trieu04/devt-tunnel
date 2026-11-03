// Package secret defines how the core reads secrets. Values live only in the OS keychain.
package secret

import (
	"errors"
	"sync"
)

var (
	// ErrNotFound means the keychain works but has no such entry: the tunnel shows Needs token.
	ErrNotFound = errors.New("secret not found")
	// ErrUnavailable means no keychain could be reached: the tunnel shows Keyring unavailable.
	ErrUnavailable = errors.New("keychain unavailable")
)

type Store interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// Memory is a Store for tests and development profiles.
type Memory struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *Memory) Get(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *Memory) Set(name, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[name] = value
	return nil
}

func (m *Memory) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, name)
	return nil
}
