//go:build !windows

package daemon_test

import (
	"syscall"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func TestCrashRecoveryAndSignals(t *testing.T) {
	s := isolated(t)
	s.save(t)
	p := s.start(t, "0")
	c := s.dial(t)
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	var status supervisor.Status
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, &status)
	if err := p.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	call(t, c, api.DaemonStatusMethod, api.Empty{}, nil)
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-p.done
	c.Close()
	p = s.start(t, "0")
	c = s.dial(t)
	defer c.Close()
	var overview api.Overview
	call(t, c, "config.overview", api.Empty{}, &overview)
	if len(overview.Orphans) != 1 || overview.Orphans[0].PID != status.PID {
		t.Fatalf("orphans: %+v", overview.Orphans)
	}
	call(t, c, "orphan.stop", api.OrphanParams{PID: status.PID}, nil)
	if _, err := platform.ProcessIdentity(status.PID); err == nil {
		t.Fatal("orphan survived stop")
	}
	call(t, c, "tunnel.start", api.IDParams{ID: "sleep"}, nil)
	call(t, c, "tunnel.waitReady", api.IDParams{ID: "sleep"}, &status)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	awaitExit(t, p)
	if _, err := platform.ProcessIdentity(status.PID); err == nil {
		t.Fatal("tunnel survived SIGTERM")
	}
}
