//go:build linux

package platform

import (
	"context"
	"errors"
)

type unavailableUserAuth struct{}

func NewUserAuth() UserAuth                 { return unavailableUserAuth{} }
func (unavailableUserAuth) Available() bool { return false }
func (unavailableUserAuth) Confirm(context.Context, string) error {
	return errors.New("OS user authentication is unavailable")
}
