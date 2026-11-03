package platform

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

// TestMain lets the test binary act as a child process that can spawn a grandchild.
func TestMain(m *testing.M) {
	switch os.Getenv("DTT_TEST_ROLE") {
	case "sleep":
		fmt.Println("started")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "parent":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "DTT_TEST_ROLE=sleep")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Printf("grandchild=%d\n", child.Process.Pid)
		child.Wait()
		os.Exit(0)
	case "exit7":
		fmt.Println("bye")
		os.Exit(7)
	}
	os.Exit(m.Run())
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func start(t *testing.T, role string, out *syncBuffer) supervisor.Process {
	t.Helper()
	p, err := Runner{}.Start(supervisor.SpawnRequest{
		Path: os.Args[0], Env: append(os.Environ(), "DTT_TEST_ROLE="+role), Output: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(time.Second) })
	return p
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestExitAndOutput(t *testing.T) {
	out := &syncBuffer{}
	p := start(t, "exit7", out)
	err := p.Wait()
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), "bye") {
		t.Errorf("output %q", out.String())
	}
}

func TestStopEndsWholeTree(t *testing.T) {
	out := &syncBuffer{}
	p := start(t, "parent", out)
	var grandchild int
	waitFor(t, func() bool {
		for _, line := range strings.Split(out.String(), "\n") {
			if v, ok := strings.CutPrefix(line, "grandchild="); ok {
				grandchild, _ = strconv.Atoi(strings.TrimSpace(v))
				return grandchild > 0
			}
		}
		return false
	}, "grandchild pid")
	if _, err := ProcessIdentity(grandchild); err != nil {
		t.Fatalf("grandchild not running: %v", err)
	}
	if err := p.Stop(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, err := ProcessIdentity(grandchild); return err != nil }, "grandchild to exit")
}

func TestIdentityStableWhileRunningAndGoneAfter(t *testing.T) {
	p := start(t, "sleep", &syncBuffer{})
	id, err := ProcessIdentity(p.PID())
	if err != nil {
		t.Fatal(err)
	}
	if id != p.Identity() || id == "" {
		t.Errorf("identity %q vs %q", id, p.Identity())
	}
	p.Stop(2 * time.Second)
	if _, err := ProcessIdentity(p.PID()); err == nil {
		t.Error("identity of exited process should fail")
	}
}

func TestIdentitySurvivesExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	p, err := Runner{}.Start(supervisor.SpawnRequest{
		Path: "/bin/sh", Args: []string{"-c", "sleep 0.2; exec sleep 30"}, Env: os.Environ(), Output: &syncBuffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(time.Second) })
	time.Sleep(500 * time.Millisecond)
	got, err := ProcessIdentity(p.PID())
	if err != nil || got != p.Identity() {
		t.Errorf("identity changed across exec: %q → %q, %v", p.Identity(), got, err)
	}
}

func TestPaths(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := env(nil)
	join := filepath.Join
	cases := []struct {
		name, got, want string
	}{
		{"mac config", configDir("darwin", "/h", none), join("/h", ".config", "devt-tunnel")},
		{"mac state", stateDir("darwin", "/h", none), join("/h", "Library", "Application Support", "devt-tunnel")},
		{"linux default", configDir("linux", "/h", none), join("/h", ".config", "devt-tunnel")},
		{"linux xdg", configDir("linux", "/h", env(map[string]string{"XDG_CONFIG_HOME": "/x"})), join("/x", "devt-tunnel")},
		{"linux state", stateDir("linux", "/h", none), join("/h", ".local", "state", "devt-tunnel")},
		{"win config", configDir("windows", "/h", env(map[string]string{"APPDATA": "/a"})), join("/a", "devt-tunnel")},
		{"win state", stateDir("windows", "/h", env(map[string]string{"LOCALAPPDATA": "/l"})), join("/l", "devt-tunnel")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestCleanEnviron(t *testing.T) {
	plain := []string{"LD_LIBRARY_PATH=/keep", "HOME=/h"}
	if got := CleanEnviron(plain); len(got) != 2 {
		t.Errorf("outside AppImage changed env: %v", got)
	}
	got := CleanEnviron([]string{"APPDIR=/tmp/app", "LD_LIBRARY_PATH=/tmp/app/lib", "GTK_PATH=/x", "HOME=/h"})
	if len(got) != 1 || got[0] != "HOME=/h" {
		t.Errorf("env %v", got)
	}
}

func TestResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix executable")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "mytool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tools := &Tools{Overrides: map[string]string{"pinned": "/opt/pinned"}}
	tools.once.Do(func() { tools.path = dir })
	if got, err := tools.Resolve("mytool"); err != nil || got != tool {
		t.Errorf("got %q, %v", got, err)
	}
	if got, _ := tools.Resolve("pinned"); got != "/opt/pinned" {
		t.Errorf("override %q", got)
	}
	if _, err := tools.Resolve("nosuchtool"); err == nil {
		t.Error("missing tool should fail")
	}
}

func TestLoginShellPATHParsesNoisyOutput(t *testing.T) {
	m := pathMarker.FindSubmatch([]byte("welcome!\n__DTT__/a:/b__DTT__"))
	if m == nil || string(m[1]) != "/a:/b" {
		t.Errorf("match %q", m)
	}
}
