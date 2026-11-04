package platform

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configure hides the console window that would otherwise flash for each tunnel.
func configure(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	// Go's argument escaping does not match cmd.exe's rules, so pass the line through as written.
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") && len(cmd.Args) == 5 && cmd.Args[3] == "/c" {
		attr.CmdLine = `cmd.exe /d /s /c "` + cmd.Args[4] + `"`
	}
	cmd.SysProcAttr = attr
}

// osProcess wraps a Job Object. Closing the last handle ends the tree,
// which also covers the app crashing.
type osProcess struct {
	pid int
	job windows.Handle
}

func attach(cmd *exec.Cmd) osProcess {
	bare := osProcess{pid: cmd.Process.Pid}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return bare
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return bare
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return bare
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return bare
	}
	return osProcess{pid: cmd.Process.Pid, job: job}
}

// terminate has no graceful form: a hidden GUI child cannot receive a console control event.
func (p osProcess) terminate() error {
	if p.job == 0 {
		return KillOrphan(p.pid) // no tree to end, but the process itself must still stop
	}
	return windows.TerminateJobObject(p.job, 1)
}

func (p osProcess) kill() error { return p.terminate() }

func (p osProcess) exited() {
	if p.job != 0 {
		windows.CloseHandle(p.job)
	}
}

// ProcessIdentity returns a value that changes if the PID is reused.
func ProcessIdentity(pid int) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return "", err
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), nil
}

// KillOrphan ends a process from an earlier run.
func KillOrphan(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

func StopOrphan(pid int, identity string, grace time.Duration) error {
	current, err := ProcessIdentity(pid)
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("process %d identity changed", pid)
	}
	if err := KillOrphan(pid); err != nil {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if _, err := ProcessIdentity(pid); err != nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := ProcessIdentity(pid); err == nil {
		return fmt.Errorf("process %d is still alive", pid)
	}
	return nil
}
