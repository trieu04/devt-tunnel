//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// configure puts the child in its own process group, so that stopping reaches anything it spawned.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = sysProcAttr()
}

type osProcess struct{ pgid int }

func attach(cmd *exec.Cmd) osProcess { return osProcess{pgid: cmd.Process.Pid} }

func (p osProcess) terminate() error { return signalGroup(p.pgid, syscall.SIGTERM) }
func (p osProcess) kill() error      { return signalGroup(p.pgid, syscall.SIGKILL) }

// exited clears anything the leader left behind in its group.
func (p osProcess) exited() { signalGroup(p.pgid, syscall.SIGKILL) }

func signalGroup(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// ProcessIdentity returns a value that changes if the PID is reused. It uses only the start
// time: the command name changes when a process execs, as a shell or a python shim does.
func ProcessIdentity(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", fmt.Errorf("process %d: %w", pid, err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("process %d not found", pid)
	}
	return id, nil
}

// StopOrphan verifies the process identity, terminates its group, then forces it after the grace period.
func StopOrphan(pid int, identity string, grace time.Duration) error {
	current, err := ProcessIdentity(pid)
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("process %d identity changed", pid)
	}
	if err := signalGroup(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if _, err := ProcessIdentity(pid); err != nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := signalGroup(pid, syscall.SIGKILL); err != nil {
		return err
	}
	for i := 0; i < 40; i++ {
		if _, err := ProcessIdentity(pid); err != nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("process %d is still alive", pid)
}

func KillOrphan(pid int) error {
	identity, err := ProcessIdentity(pid)
	if err != nil {
		return err
	}
	return StopOrphan(pid, identity, 0)
}
