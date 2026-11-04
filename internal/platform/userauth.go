package platform

import "context"

type UserAuth interface {
	Available() bool
	Confirm(context.Context, string) error
}
