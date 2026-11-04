package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestControlSocketPaths(t *testing.T) {
	for _, tc := range []struct{ os, override, xdg, want string }{
		{"linux", "", "/run/user/123", "/run/user/123/devt-tunnel/ctl.sock"},
		{"darwin", "", "/ignored", "/state/run/ctl.sock"},
		{"windows", "", "/ignored", "/state/run/ctl.sock"},
		{"linux", "/custom/run/ctl.sock", "/ignored", "/custom/run/ctl.sock"},
	} {
		got, err := controlSocket(tc.os, "/state", "/tmp", func(k string) string {
			if k == "DTT_SOCKET" {
				return tc.override
			}
			return tc.xdg
		})
		want, _ := filepath.Abs(filepath.FromSlash(tc.want))
		if err != nil || got != want {
			t.Errorf("%+v: %q, %v; want %q", tc, got, err, want)
		}
	}
	get := func(string) string { return "" }
	a, err := controlSocket("linux", "/"+strings.Repeat("a", 150), "/tmp", get)
	b, _ := controlSocket("linux", "/"+strings.Repeat("b", 150), "/tmp", get)
	again, _ := controlSocket("linux", "/"+strings.Repeat("a", 150), "/tmp", get)
	if err != nil || len(a) > 100 || a == b || a != again {
		t.Fatalf("fallback: %q %q %v", a, b, err)
	}
}

func TestPrivateDirAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	if err := PreparePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		shared := filepath.Join(t.TempDir(), "shared")
		os.Mkdir(shared, 0o755)
		if PreparePrivateDir(shared) == nil {
			t.Fatal("accepted shared directory")
		}
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if PreparePrivateDir(link) == nil {
			t.Fatal("accepted symlink")
		}
	}
	path := filepath.Join(dir, "daemon.lock")
	release, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := AcquireLock(path); !errors.Is(err, ErrLocked) {
		if other != nil {
			other()
		}
		t.Fatalf("second lock: %v", err)
	}
	release()
	release()
	release, err = AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestLockReleasedAfterProcessExit(t *testing.T) {
	if path := os.Getenv("DTT_LOCK_TEST_CHILD"); path != "" {
		if _, err := AcquireLock(path); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "daemon.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedAfterProcessExit$")
	cmd.Env = append(os.Environ(), "DTT_LOCK_TEST_CHILD="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, output)
	}
	release, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
