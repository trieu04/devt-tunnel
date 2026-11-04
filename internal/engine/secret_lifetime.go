package engine

import (
	"context"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

// Native credential calls cannot be cancelled; stop their engine continuations when the engine shuts down.
type lifetimeSecrets struct {
	inner secret.Store
	done  <-chan struct{}
}

func (s *lifetimeSecrets) call(fn func() (string, error)) (string, error) {
	select {
	case <-s.done:
		return "", context.Canceled
	default:
	}
	type result struct {
		value string
		err   error
	}
	finished := make(chan result, 1)
	go func() {
		value, err := fn()
		finished <- result{value, err}
	}()
	select {
	case <-s.done:
		return "", context.Canceled
	case r := <-finished:
		select {
		case <-s.done:
			return "", context.Canceled
		default:
			return r.value, r.err
		}
	}
}

func (s *lifetimeSecrets) Get(name string) (string, error) {
	return s.call(func() (string, error) { return s.inner.Get(name) })
}
func (s *lifetimeSecrets) Set(name, value string) error {
	_, err := s.call(func() (string, error) { return "", s.inner.Set(name, value) })
	return err
}
func (s *lifetimeSecrets) Delete(name string) error {
	_, err := s.call(func() (string, error) { return "", s.inner.Delete(name) })
	return err
}
