// Package secret defines how the core reads secrets. Values live only in the OS keychain.
package secret

import (
	"errors"
	"sync"
	"time"
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

type presenceStore interface {
	Has(name string) (bool, error)
}

// Has checks presence without requiring callers to read or retain a value.
func Has(store Store, name string) (bool, error) {
	if cached, ok := store.(presenceStore); ok {
		return cached.Has(name)
	}
	_, err := store.Get(name)
	if err != nil {
		return false, err
	}
	return true, nil
}

type presence struct {
	present bool
	err     error
	at      time.Time
}

// Cached caches keychain presence while always forwarding value reads to the inner store.
type Cached struct {
	Inner Store
	Now   func() time.Time
	Used  func(string)

	mu       sync.Mutex
	presence map[string]presence
}

func (c *Cached) clock() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Cached) Get(name string) (string, error) {
	value, err := c.Inner.Get(name)
	if err == nil && c.Used != nil {
		c.Used(name)
	}
	return value, err
}

func (c *Cached) Has(name string) (bool, error) {
	now := c.clock()
	c.mu.Lock()
	item, ok := c.presence[name]
	if ok && (!errors.Is(item.err, ErrUnavailable) || now.Sub(item.at) < 30*time.Second) {
		c.mu.Unlock()
		return item.present, item.err
	}
	c.mu.Unlock()

	_, err := c.Inner.Get(name)
	item = presence{present: err == nil, err: err, at: now}
	c.mu.Lock()
	if c.presence == nil {
		c.presence = map[string]presence{}
	}
	c.presence[name] = item
	c.mu.Unlock()
	return item.present, item.err
}

func (c *Cached) Set(name, value string) error {
	if err := c.Inner.Set(name, value); err != nil {
		return err
	}
	c.mu.Lock()
	if c.presence == nil {
		c.presence = map[string]presence{}
	}
	c.presence[name] = presence{present: true, at: c.clock()}
	c.mu.Unlock()
	return nil
}

func (c *Cached) Delete(name string) error {
	if err := c.Inner.Delete(name); err != nil {
		return err
	}
	c.mu.Lock()
	if c.presence == nil {
		c.presence = map[string]presence{}
	}
	c.presence[name] = presence{err: ErrNotFound, at: c.clock()}
	c.mu.Unlock()
	return nil
}

func (c *Cached) Forget() {
	c.mu.Lock()
	c.presence = nil
	c.mu.Unlock()
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
