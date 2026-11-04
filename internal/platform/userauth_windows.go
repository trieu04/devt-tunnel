//go:build windows

package platform

import (
	"context"
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsUserAuth struct{}

type credUIInfo struct {
	Size    uint32
	Parent  windows.Handle
	Message *uint16
	Caption *uint16
	Banner  windows.Handle
}

var (
	credui                          = windows.NewLazySystemDLL("credui.dll")
	procPromptForWindowsCredentials = credui.NewProc("CredUIPromptForWindowsCredentialsW")
	procUnpackAuthenticationBuffer  = credui.NewProc("CredUnPackAuthenticationBufferW")
	advapi                          = windows.NewLazySystemDLL("advapi32.dll")
	procLogonUser                   = advapi.NewProc("LogonUserW")
	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	procCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
)

func NewUserAuth() UserAuth             { return windowsUserAuth{} }
func (windowsUserAuth) Available() bool { return true }

func (windowsUserAuth) Confirm(ctx context.Context, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	caption, _ := windows.UTF16PtrFromString("devt-tunnel")
	message, _ := windows.UTF16PtrFromString(reason)
	info := credUIInfo{Size: uint32(unsafe.Sizeof(credUIInfo{})), Caption: caption, Message: message}
	var packageID uint32
	var buffer unsafe.Pointer
	var size uint32
	result, _, _ := procPromptForWindowsCredentials.Call(uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&packageID)), 0, 0, uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&size)), 0, 0)
	if result != 0 {
		if result == uintptr(windows.ERROR_CANCELLED) {
			return errors.New("authentication cancelled")
		}
		return syscall.Errno(result)
	}
	defer procCoTaskMemFree.Call(uintptr(buffer))
	username := make([]uint16, 512)
	domain := make([]uint16, 512)
	password := make([]uint16, 512)
	usernameLen, domainLen, passwordLen := uint32(len(username)), uint32(len(domain)), uint32(len(password))
	ok, _, unpackErr := procUnpackAuthenticationBuffer.Call(0, uintptr(buffer), uintptr(size), uintptr(unsafe.Pointer(&username[0])), uintptr(unsafe.Pointer(&usernameLen)), uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&domainLen)), uintptr(unsafe.Pointer(&password[0])), uintptr(unsafe.Pointer(&passwordLen)))
	if ok == 0 {
		return unpackErr
	}
	defer clear(password)
	var token windows.Handle
	ok, _, logonErr := procLogonUser.Call(uintptr(unsafe.Pointer(&username[0])), uintptr(unsafe.Pointer(&domain[0])), uintptr(unsafe.Pointer(&password[0])), 2, 0, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		return logonErr
	}
	return windows.CloseHandle(token)
}
