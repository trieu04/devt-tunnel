package engine

import (
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type secretIndexStore interface {
	List() []secret.Entry
	Upsert(string, secret.Kind) error
	Delete(string) error
	Rename(string, string) error
	MarkUsed(string)
}

func (s *Engine) withSecretIndex(operation func(secretIndexStore) error) error {
	if s.beforeSecretIndex != nil {
		s.beforeSecretIndex()
	}
	s.secretIndexMu.Lock()
	defer s.secretIndexMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	if err := operation(s.secretIndex); err != nil {
		return err
	}
	return s.lifetimeError()
}

func (s *Engine) markSecretUsed(name string) {
	_ = s.withSecretIndex(func(index secretIndexStore) error {
		index.MarkUsed(name)
		return nil
	})
}
