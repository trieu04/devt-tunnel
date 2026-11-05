//go:build darwin

package platform

import "testing"

func TestDeviceOwnerAuthenticationPolicy(t *testing.T) {
	if deviceOwnerAuthenticationPolicy != 2 {
		t.Fatalf("LocalAuthentication policy = %d, want LAPolicyDeviceOwnerAuthentication (2)", deviceOwnerAuthenticationPolicy)
	}
}
