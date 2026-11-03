package platform

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

const keyringService = "devt-tunnel"

// Keychain stores secrets in the OS keychain.
type Keychain struct{}

func (Keychain) Get(name string) (string, error) {
	value, err := keyring.Get(keyringService, name)
	return value, mapKeyringError(err)
}

func (Keychain) Set(name, value string) error {
	return mapKeyringError(keyring.Set(keyringService, name, value))
}

func (Keychain) Delete(name string) error {
	err := keyring.Delete(keyringService, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return mapKeyringError(err)
}

func mapKeyringError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return secret.ErrNotFound
	}
	return fmt.Errorf("%w: %v", secret.ErrUnavailable, err)
}
