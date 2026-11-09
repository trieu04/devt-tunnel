package daemon_test

import (
	"context"
	"errors"
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

type blockingMutationStore struct{ blockingStore }

func (s *blockingMutationStore) Set(string, string) error { _, err := s.Get(""); return err }
func (s *blockingMutationStore) Delete(string) error      { _, err := s.Get(""); return err }

func TestShutdownDuringBlockedMutation(t *testing.T) {
	for _, request := range []struct {
		method string
		params any
	}{
		{"tunnel.start", api.IDParams{ID: "secret"}},
		{"tunnel.restart", api.IDParams{ID: "secret"}},
		{"group.start", api.IDParams{ID: "group"}},
		{"secret.set", api.SetSecretParams{Name: "disposable", Value: "test"}},
		{"secret.delete", api.SecretNameParams{Name: "disposable"}},
		{"secret.rename", api.RenameSecretParams{From: "disposable", To: "renamed"}},
	} {
		for _, mode := range []string{"shutdown", "cancel"} {
			t.Run(request.method+"/"+mode, func(t *testing.T) {
				s := isolated(t)
				path := filepath.Join(s.root, "config", "tunnels.toml")
				cfg := model.Config{Groups: []model.Group{{ID: "group", Name: "Group"}}, Tunnels: []model.Tunnel{{ID: "secret", Name: "Secret", Groups: []string{"group"}, Type: model.TypeCommand, Command: &model.Command{Line: "unused", SecretEnvs: map[string]string{"TOKEN": "disposable"}}}}}
				if err := config.Save(path, cfg); err != nil {
					t.Fatal(err)
				}
				store := &blockingMutationStore{blockingStore{called: make(chan struct{}, 1), release: make(chan struct{})}}
				ctx, cancel := context.WithCancel(context.Background())
				finished := make(chan error, 1)
				go func() {
					finished <- daemon.Run(ctx, daemon.Options{Socket: s.socket, Engine: engine.Options{ConfigPath: path, SettingsPath: filepath.Join(s.root, "settings.toml"), StateDir: filepath.Join(s.root, "state"), Secrets: store, AskpassHelper: "unused"}})
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
				mutationDone := make(chan error, 1)
				go func() { mutationDone <- c.Call(ctx, request.method, request.params, nil) }()
				select {
				case <-store.called:
				case <-time.After(2 * time.Second):
					t.Fatal("mutation did not reach secret store")
				}
				bounded, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				err := c.Call(bounded, api.DaemonShutdown, api.ShutdownParams{OnlyIfIdle: true}, nil)
				var rpc *api.Error
				if !errors.As(err, &rpc) || rpc.Data.Kind != api.Conflict {
					t.Fatalf("idle shutdown during mutation: %v", err)
				}
				call(t, c, api.DaemonStatusMethod, api.Empty{}, nil)
				if mode == "cancel" {
					cancel()
				} else if err := c.Call(bounded, api.DaemonShutdown, api.ShutdownParams{}, nil); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					if err != nil {
						t.Fatal(err)
					}
				case <-bounded.Done():
					t.Fatal("shutdown waited for native credential operation")
				}
				select {
				case <-mutationDone:
				case <-bounded.Done():
					t.Fatal("mutation client did not finish")
				}
			})
		}
	}
}
