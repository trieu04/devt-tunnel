package platform

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var ErrLocked = errors.New("daemon already running")

func ControlSocket() (string, error) {
	state, err := StateDir()
	if err != nil {
		return "", err
	}
	return controlSocket(runtime.GOOS, state, os.TempDir(), os.Getenv)
}

func controlSocket(goos, state, temp string, getenv func(string) string) (string, error) {
	path := getenv("DTT_SOCKET")
	if path == "" && goos == "linux" && getenv("XDG_RUNTIME_DIR") != "" {
		path = filepath.Join(getenv("XDG_RUNTIME_DIR"), appDir, "ctl.sock")
	}
	if path == "" {
		path = filepath.Join(state, "run", "ctl.sock")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if len(path) <= 100 {
		return path, nil
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", os.Getuid(), path)))
	name := fmt.Sprintf("dtt-%x", hash[:6])
	path = filepath.Join(temp, name, "ctl.sock")
	if len(path) > 100 && goos != "windows" {
		path = filepath.Join("/tmp", name, "ctl.sock")
	}
	if len(path) > 100 {
		return "", errors.New("OS temporary directory is too long for a control socket")
	}
	return path, nil
}
