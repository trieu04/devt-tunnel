package engine

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestPortCheckFreeOccupiedAndOwnRunning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	tunnel := model.Tunnel{ID: "ssh", Name: "SSH", Type: model.TypeSSH, SSH: &model.SSH{Host: "test", Flags: []string{"-N"}}, Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: address, Target: "localhost:80"}}}
	e, runner, _ := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
	assertOccupied := func(want bool) {
		t.Helper()
		for name, findings := range map[string][]checks.Finding{"draft": e.EvaluateDraft(tunnel).Findings, "saved": e.Findings(tunnel.ID)} {
			got := false
			for _, finding := range findings {
				got = got || strings.Contains(finding.Message, "already in use")
			}
			if got != want {
				t.Fatalf("%s occupied = %v, want %v: %#v", name, got, want, findings)
			}
		}
	}
	assertOccupied(true)
	listener.Close()
	if err := e.Start(tunnel.ID); err != nil {
		t.Fatal(err)
	}
	receive(t, runner.started)
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.WaitReady(ctx, tunnel.ID); err != nil {
		t.Fatal(err)
	}
	assertOccupied(false)
	changed := tunnel
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	changed.Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: other.Addr().String(), Target: "localhost:80"}}
	found := false
	for _, f := range e.EvaluateDraft(changed).Findings {
		found = found || strings.Contains(f.Message, "already in use")
	}
	if !found {
		t.Fatal("editing an active tunnel suppressed a different occupied port")
	}
	e.Stop(tunnel.ID)
	assertOccupied(true)
	listener.Close()
	assertOccupied(false)
}

func TestPortCheckOnlyDialsLoopback(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	d := e.checkDeps(model.Tunnel{})
	for _, address := range []string{"192.0.2.1:80", "example.invalid:80", "/tmp/socket"} {
		if d.Listening(address) {
			t.Errorf("unexpected listener for %s", address)
		}
	}
}
