package engine

import (
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestShutdownCancelsSecretStartAndSpawnPreparation(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "start presence", true: "spawn value"}[cached], func(t *testing.T) {
			tunnel := commandTunnel()
			tunnel.ID = "t"
			tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "disposable"}
			e, runner, memory := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
			if cached {
				if err := memory.Set("disposable", "test"); err != nil {
					t.Fatal(err)
				}
				if _, err := e.secrets.Has("disposable"); err != nil {
					t.Fatal(err)
				}
			}
			blocked := &blockingSecrets{called: make(chan struct{}, 1), release: make(chan struct{})}
			e.secrets.Inner.(*lifetimeSecrets).inner = blocked
			defer close(blocked.release)
			startDone := make(chan error, 1)
			go func() { startDone <- e.Start("t") }()
			receive(t, blocked.called)
			shutdownDone := make(chan struct{})
			go func() { e.Shutdown(); close(shutdownDone) }()
			select {
			case <-shutdownDone:
			case <-time.After(time.Second):
				t.Fatal("shutdown waited for credentials")
			}
			receive(t, startDone)
			select {
			case <-runner.started:
				t.Fatal("spawned while credentials were blocked")
			default:
			}
			if err := e.Start("t"); err == nil {
				t.Fatal("start accepted after shutdown")
			}
		})
	}
}
