package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type pausedSecretIndex struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   map[string]int
}

func newPausedSecretIndex() *pausedSecretIndex {
	return &pausedSecretIndex{entered: make(chan struct{}), release: make(chan struct{}), calls: map[string]int{}}
}

func (i *pausedSecretIndex) record(method string) {
	i.mu.Lock()
	i.calls[method]++
	i.mu.Unlock()
}

func (i *pausedSecretIndex) count(method string) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.calls[method]
}

func (i *pausedSecretIndex) List() []secret.Entry {
	i.record("list")
	i.once.Do(func() {
		close(i.entered)
		<-i.release
	})
	return nil
}

func (i *pausedSecretIndex) Upsert(string, secret.Kind) error { i.record("upsert"); return nil }
func (i *pausedSecretIndex) Delete(string) error              { i.record("delete"); return nil }
func (i *pausedSecretIndex) Rename(string, string) error      { i.record("rename"); return nil }
func (i *pausedSecretIndex) MarkUsed(string)                  { i.record("mark-used") }

func blockSecretIndex(t *testing.T, e *Engine, index *pausedSecretIndex) chan []api.SecretView {
	t.Helper()
	e.secretIndex = index
	blocked := make(chan []api.SecretView, 1)
	go func() { blocked <- e.ListSecrets() }()
	receive(t, index.entered)
	return blocked
}

func shutdownWithoutWaitingForIndex(t *testing.T, e *Engine, release chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	go func() { e.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("shutdown waited for secret index I/O")
	}
}

func TestShutdownCancelsQueuedSecretIndexMutations(t *testing.T) {
	for _, method := range []string{"set", "delete", "rename"} {
		t.Run(method, func(t *testing.T) {
			tunnel := commandTunnel()
			tunnel.ID = "t"
			tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "old"}
			e, _, memory := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
			if err := memory.Set("old", "value"); err != nil {
				t.Fatal(err)
			}
			index := newPausedSecretIndex()
			blocked := blockSecretIndex(t, e, index)
			e.secrets.Used = nil
			reached := make(chan struct{}, 1)
			e.beforeSecretIndex = func() { reached <- struct{}{} }
			result := make(chan error, 1)
			go func() {
				switch method {
				case "set":
					result <- e.SetSecret("new", "value", secret.KindEnv)
				case "delete":
					result <- e.DeleteSecret("old")
				case "rename":
					result <- e.RenameSecret("old", "new")
				}
			}()
			receive(t, reached)
			shutdownWithoutWaitingForIndex(t, e, index.release)
			close(index.release)
			if err := receive(t, result); !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "metadata") {
				t.Fatalf("queued %s continued after shutdown: %v", method, err)
			}
			if views := receive(t, blocked); len(views) != 0 {
				t.Fatalf("blocked list continued after shutdown: %+v", views)
			}
			indexMethod := map[string]string{"set": "upsert", "delete": "delete", "rename": "rename"}[method]
			if got := index.count(indexMethod); got != 0 {
				t.Fatalf("issued queued %s index I/O: %d", method, got)
			}
		})
	}
}

func TestShutdownCancelsQueuedSecretIndexReads(t *testing.T) {
	for _, method := range []string{"mark-used", "list"} {
		t.Run(method, func(t *testing.T) {
			e, _, memory := newTestEngine(t, model.Config{})
			if err := memory.Set("used", "value"); err != nil {
				t.Fatal(err)
			}
			index := newPausedSecretIndex()
			blocked := blockSecretIndex(t, e, index)
			reached := make(chan struct{}, 1)
			e.beforeSecretIndex = func() { reached <- struct{}{} }
			read := make(chan []api.SecretView, 1)
			used := make(chan error, 1)
			if method == "mark-used" {
				go func() { _, err := e.secrets.Get("used"); used <- err }()
			} else {
				go func() { read <- e.ListSecrets() }()
			}
			receive(t, reached)
			shutdownWithoutWaitingForIndex(t, e, index.release)
			close(index.release)
			if views := receive(t, blocked); len(views) != 0 {
				t.Fatalf("blocked list continued after shutdown: %+v", views)
			}
			if method == "mark-used" {
				if err := receive(t, used); err != nil {
					t.Fatal(err)
				}
				if got := index.count("mark-used"); got != 0 {
					t.Fatalf("issued queued mark-used I/O: %d", got)
				}
			} else if views := receive(t, read); len(views) != 0 {
				t.Fatalf("queued list continued after shutdown: %+v", views)
			}
		})
	}
}
