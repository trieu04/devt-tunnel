package engine

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type blockingSecrets struct {
	called  chan struct{}
	release chan struct{}
}

func (s *blockingSecrets) Get(string) (string, error) {
	select {
	case s.called <- struct{}{}:
	default:
	}
	<-s.release
	return "", secret.ErrNotFound
}
func (*blockingSecrets) Set(string, string) error { return errors.New("unexpected secret write") }
func (*blockingSecrets) Delete(string) error      { return errors.New("unexpected secret delete") }

func TestActivityDoesNotConsultSecretsOrSettings(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "pending secret start"}[pending], func(t *testing.T) {
			tunnel := commandTunnel()
			tunnel.ID = "t"
			tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "disposable"}
			e, _, _ := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
			if pending {
				if err := e.Start("t"); err != nil {
					t.Fatal(err)
				}
			}
			blocked := &blockingSecrets{called: make(chan struct{}, 1), release: make(chan struct{})}
			e.secrets.Inner = blocked
			e.ForgetSecrets()
			presentationDone := make(chan struct{})
			go func() { e.manager.Statuses(); close(presentationDone) }()
			defer func() { close(blocked.release); <-presentationDone }()
			receive(t, blocked.called)
			e.mu.Lock()
			defer e.mu.Unlock()
			result := make(chan bool, 1)
			go func() { result <- e.Active() }()
			select {
			case active := <-result:
				if active {
					t.Fatal("stopped or pending-secret tunnel counted active")
				}
			case <-time.After(time.Second):
				t.Fatal("activity waited on secrets or settings lock")
			}
			statuses := make(chan []supervisor.Status, 1)
			go func() { statuses <- e.Statuses() }()
			select {
			case got := <-statuses:
				if len(got) != 1 || got[0].State != supervisor.StateStopped {
					t.Fatalf("runtime status: %+v", got)
				}
			case <-time.After(time.Second):
				t.Fatal("runtime status blocked")
			}
			select {
			case <-blocked.called:
				t.Fatal("activity read secret store")
			default:
			}
		})
	}
}

type pausedConfigStore struct {
	configurationStore
	committed  chan struct{}
	release    chan struct{}
	secondRead chan struct{}
	updates    atomic.Int32
}

func (s *pausedConfigStore) Load() (model.Config, error) {
	n := s.updates.Add(1)
	if n == 2 {
		close(s.secondRead)
	}
	cfg, err := s.configurationStore.Load()
	if n == 1 {
		close(s.committed)
		<-s.release
	}
	return cfg, err
}

func (s *pausedConfigStore) Update(change func(*model.Config) error) (model.Config, error) {
	n := s.updates.Add(1)
	if n == 2 {
		close(s.secondRead)
	}
	cfg, err := s.configurationStore.Update(change)
	if n == 1 {
		close(s.committed)
		<-s.release
	}
	return cfg, err
}

func TestConcurrentConfigCommitsCannotApplyStaleSnapshot(t *testing.T) {
	for _, firstReload := range []bool{false, true} {
		t.Run(map[bool]string{false: "save then save", true: "reload then save"}[firstReload], func(t *testing.T) {
			testConcurrentConfigCommits(t, firstReload)
		})
	}
}

func testConcurrentConfigCommits(t *testing.T, firstReload bool) {
	e, _, _ := newTestEngine(t, model.Config{})
	store := &pausedConfigStore{configurationStore: e.configStore, committed: make(chan struct{}), release: make(chan struct{}), secondRead: make(chan struct{})}
	e.configStore = store
	var release sync.Once
	unblock := func() { release.Do(func() { close(store.release) }) }
	defer unblock()
	first := make(chan error, 1)
	go func() {
		if firstReload {
			e.Reload()
			first <- nil
			return
		}
		tunnel := commandTunnel()
		tunnel.Name = "first"
		_, err := e.SaveTunnel(tunnel, false)
		first <- err
	}()
	select {
	case <-store.committed:
	case <-time.After(time.Second):
		t.Fatal("first save did not commit")
	}
	secondStarted := make(chan struct{})
	second := make(chan error, 1)
	go func() {
		close(secondStarted)
		tunnel := commandTunnel()
		tunnel.Name = "second"
		_, err := e.SaveTunnel(tunnel, false)
		second <- err
	}()
	<-secondStarted
	select {
	case <-store.secondRead:
		t.Fatal("second save passed a committed but unapplied snapshot")
	case <-time.After(50 * time.Millisecond):
	}
	activity := make(chan bool, 1)
	go func() { activity <- e.Active() }()
	select {
	case <-activity:
	case <-time.After(time.Second):
		t.Fatal("config mutation blocked unrelated activity query")
	}
	unblock()
	for _, result := range []chan error{first, second} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("save blocked")
		}
	}
	disk, err := config.Load(e.configPath)
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 2
	if firstReload {
		wantCount = 1
	}
	if got := e.manager.Config(); !reflect.DeepEqual(got, disk) || len(got.Tunnels) != wantCount {
		t.Fatalf("manager/disk diverged: manager=%+v disk=%+v", got, disk)
	}
}

func TestShutdownFencesQueuedConfigOperations(t *testing.T) {
	for _, mode := range []struct {
		name                string
		firstReload, reload bool
	}{
		{"save-save", false, false}, {"save-reload", false, true}, {"reload-save", true, false}, {"reload-reload", true, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			e, _, _ := newTestEngine(t, model.Config{})
			store := &pausedConfigStore{configurationStore: e.configStore, committed: make(chan struct{}), release: make(chan struct{}), secondRead: make(chan struct{})}
			e.configStore = store
			var release sync.Once
			unblock := func() { release.Do(func() { close(store.release) }) }
			defer unblock()
			first := make(chan error, 1)
			go func() {
				if mode.firstReload {
					first <- e.Reload()
					return
				}
				_, err := e.SaveTunnel(commandTunnel(), false)
				first <- err
			}()
			receive(t, store.committed)
			queued := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				if mode.reload {
					queued <- e.Reload()
					return
				}
				tunnel := commandTunnel()
				tunnel.Name = "queued"
				_, err := e.SaveTunnel(tunnel, false)
				queued <- err
			}()
			<-started
			select {
			case <-store.secondRead:
				t.Fatal("queued operation bypassed config lock")
			case <-time.After(50 * time.Millisecond):
			}
			shutdown := make(chan struct{})
			go func() { e.Shutdown(); close(shutdown) }()
			select {
			case <-shutdown:
			case <-time.After(time.Second):
				t.Fatal("shutdown waited for config I/O")
			}
			unblock()
			for _, result := range []chan error{first, queued} {
				if err := receive(t, result); !errors.Is(err, context.Canceled) {
					t.Fatalf("operation continued after shutdown: %v", err)
				}
			}
			if store.updates.Load() != 1 {
				t.Fatalf("issued queued config I/O after shutdown: %d", store.updates.Load())
			}
			disk, err := config.Load(e.configPath)
			if err != nil {
				t.Fatal(err)
			}
			wantDisk := 1
			if mode.firstReload {
				wantDisk = 0
			}
			if len(disk.Tunnels) != wantDisk || len(e.manager.Config().Tunnels) != 0 {
				t.Fatalf("unexpected disk/manager state: %+v / %+v", disk, e.manager.Config())
			}
		})
	}
}
