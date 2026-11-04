package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

func PreparePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing non-directory %q", path)
	}
	return nil
}

func AcquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlap windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap)
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLocked
		}
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlap); _ = f.Close() })
	}, nil
}

func Detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
}

func CheckPeer(net.Conn) error { return nil }
