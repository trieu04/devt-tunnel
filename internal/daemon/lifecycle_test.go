package daemon_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/daemon"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

type blockingStore struct {
	secret.Memory
	called  chan struct{}
	release chan struct{}
}

func (s *blockingStore) Get(string) (string, error) {
	select {
	case s.called <- struct{}{}:
	default:
	}
	<-s.release
	return "", secret.ErrNotFound
}

func TestLifecycleDoesNotReadSecrets(t *testing.T) {
	for _, mode := range []string{"cancel", "shutdown", "idle", "cancel-blocked-read", "shutdown-blocked-read", "idle-blocked-read"} {
		t.Run(mode, func(t *testing.T) {
			s := isolated(t)
			path := filepath.Join(s.root, "config", "tunnels.toml")
			if err := config.Save(path, model.Config{Tunnels: []model.Tunnel{{ID: "secret", Name: "Secret", Type: model.TypeCommand, Command: &model.Command{Line: "unused", SecretEnvs: map[string]string{"TOKEN": "disposable"}}}}}); err != nil {
				t.Fatal(err)
			}
			store := &blockingStore{called: make(chan struct{}, 1), release: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			finished := make(chan error, 1)
			go func() {
				finished <- daemon.Run(ctx, daemon.Options{Socket: s.socket, IdleExit: 100 * time.Millisecond, Engine: engine.Options{ConfigPath: path, SettingsPath: filepath.Join(s.root, "settings.toml"), StateDir: filepath.Join(s.root, "state"), Secrets: store, AskpassHelper: "unused"}})
				close(finished)
			}()
			defer func() {
				cancel()
				close(store.release)
				select {
				case <-finished:
				case <-time.After(2 * time.Second):
					t.Error("daemon did not clean up")
				}
			}()
			c := s.dial(t)
			defer c.Close()
			var status api.DaemonStatus
			call(t, c, api.DaemonStatusMethod, api.Empty{}, &status)
			if status.Active || len(status.Tunnels) != 1 || status.Tunnels[0].State != supervisor.StateStopped {
				t.Fatalf("runtime status: %+v", status)
			}
			if strings.HasSuffix(mode, "-blocked-read") {
				readDone := make(chan error, 1)
				go func() { readDone <- c.Call(ctx, "tunnel.list", api.Empty{}, nil) }()
				select {
				case <-store.called:
				case <-time.After(2 * time.Second):
					t.Fatal("query did not reach secret store")
				}
				defer func() {
					c.Close()
					select {
					case <-readDone:
					case <-time.After(2 * time.Second):
						t.Error("query client did not disconnect")
					}
				}()
			}
			switch strings.TrimSuffix(mode, "-blocked-read") {
			case "cancel":
				cancel()
			case "shutdown":
				call(t, c, api.DaemonShutdown, api.ShutdownParams{OnlyIfIdle: true}, nil)
			case "idle":
				c.Close()
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("daemon lifecycle blocked on secret store")
			}
			select {
			case <-store.called:
				t.Fatal("daemon lifecycle read secret store")
			default:
			}
		})
	}
}
