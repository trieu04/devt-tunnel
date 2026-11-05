//go:build darwin

package platform

import (
	"context"
	"errors"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type localUserAuth struct{ available bool }

const deviceOwnerAuthenticationPolicy int64 = 2

func NewUserAuth() UserAuth {
	if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return localUserAuth{}
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/LocalAuthentication.framework/LocalAuthentication", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return localUserAuth{}
	}
	return localUserAuth{available: objc.GetClass("LAContext") != 0}
}

func (a localUserAuth) Available() bool { return a.available }

func (a localUserAuth) Confirm(ctx context.Context, reason string) error {
	if !a.available {
		return errors.New("OS user authentication is unavailable")
	}
	class := objc.ID(objc.GetClass("LAContext"))
	laContext := class.Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
	if laContext == 0 {
		return errors.New("cannot create authentication context")
	}
	defer laContext.Send(objc.RegisterName("release"))
	var evaluationError objc.ID
	if !objc.Send[bool](laContext, objc.RegisterName("canEvaluatePolicy:error:"), deviceOwnerAuthenticationPolicy, &evaluationError) {
		return errors.New("device owner authentication is not configured")
	}
	reasonText := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), reason+"\x00")
	result := make(chan error, 1)
	block := objc.NewBlock(func(_ objc.Block, success bool, _ objc.ID) {
		if success {
			result <- nil
		} else {
			result <- errors.New("authentication cancelled or failed")
		}
	})
	defer block.Release()
	laContext.Send(objc.RegisterName("evaluatePolicy:localizedReason:reply:"), deviceOwnerAuthenticationPolicy, reasonText, block)
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		laContext.Send(objc.RegisterName("invalidate"))
		return ctx.Err()
	}
}
