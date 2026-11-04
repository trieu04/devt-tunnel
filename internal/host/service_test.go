package host

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type fakeAuth struct{}

func (fakeAuth) Available() bool                       { return false }
func (fakeAuth) Confirm(context.Context, string) error { return errors.New("unavailable") }

type rpcCall struct {
	method string
	params any
}

type fakeDaemon struct {
	mu       sync.Mutex
	done     chan struct{}
	closed   sync.Once
	overview api.Overview
	settings api.SettingsResult
	calls    []rpcCall
	events   func(api.Event)
}

func newFakeDaemon(path string) *fakeDaemon {
	return &fakeDaemon{done: make(chan struct{}), overview: api.Overview{ConfigPath: path}, settings: api.SettingsResult{Shortcut: "Alt+Space", UpdateChecks: true}}
}

func (f *fakeDaemon) Call(_ context.Context, method string, params, result any) error {
	select {
	case <-f.done:
		return io.EOF
	default:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rpcCall{method: method, params: params})
	switch method {
	case api.EventsSubscribe, api.DaemonShutdown, "tunnel.start", "tunnel.stop", "tunnel.startOnLaunch":
		if out, ok := result.(*api.Empty); ok {
			*out = api.Empty{}
		}
	case "config.overview":
		*(result.(*api.Overview)) = f.overview
	case "settings.get":
		*(result.(*api.SettingsResult)) = f.settings
	default:
		return errors.New("unexpected method: " + method)
	}
	return nil
}

func (f *fakeDaemon) Events(callback func(api.Event)) {
	f.mu.Lock()
	f.events = callback
	f.mu.Unlock()
}

func (f *fakeDaemon) Done() <-chan struct{} { return f.done }
func (f *fakeDaemon) Close() error          { f.closed.Do(func() { close(f.done) }); return nil }

func (f *fakeDaemon) emit(event api.Event) {
	f.mu.Lock()
	callback := f.events
	f.mu.Unlock()
	callback(event)
}

func (f *fakeDaemon) recorded(method string) []rpcCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []rpcCall
	for _, call := range f.calls {
		if call.method == method {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestServiceForwardsCallsAndEvents(t *testing.T) {
	daemon := newFakeDaemon("fake.toml")
	s := newService(func(context.Context) (daemonClient, error) { return daemon, nil }, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)

	if err := s.Start("db"); err != nil {
		t.Fatal(err)
	}
	calls := daemon.recorded("tunnel.start")
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].params, api.IDParams{ID: "db"}) {
		t.Fatalf("start calls = %#v", calls)
	}
	s.startMarked()
	s.startMarked()
	if calls := daemon.recorded("tunnel.startOnLaunch"); len(calls) != 1 {
		t.Fatalf("start marked calls = %d, want 1", len(calls))
	}

	events := make(chan api.Event, 2)
	s.mu.Lock()
	s.eventSink = func(event api.Event) { events <- event }
	s.mu.Unlock()
	prompt := api.PendingPrompt{ID: "prompt-1"}
	daemon.emit(api.Event{Type: api.EventPromptAdded, Prompt: &prompt})
	daemon.emit(api.Event{Type: api.EventTunnelsChanged})
	for _, want := range []string{api.EventPromptAdded, api.EventTunnelsChanged} {
		select {
		case event := <-events:
			if event.Type != want {
				t.Fatalf("event type = %q, want %q", event.Type, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func TestServiceCachesOverviewAndReconnects(t *testing.T) {
	first := newFakeDaemon("first.toml")
	second := newFakeDaemon("second.toml")
	var attempts atomic.Int32
	var reconnect atomic.Bool
	connect := func(context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		if !reconnect.Load() {
			return nil, errors.New("daemon unavailable")
		}
		return second, nil
	}
	s := newService(connect, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	first.Close()
	s.startMarked()

	eventually(t, 2*time.Second, func() bool {
		overview := s.Overview()
		return overview.ConfigPath == "first.toml" && strings.Contains(overview.ConfigError, "Cannot reach the tunnel service")
	})
	reconnect.Store(true)
	eventually(t, 3*time.Second, func() bool {
		overview := s.Overview()
		return overview.ConfigPath == "second.toml" && overview.ConfigError == ""
	})
	if calls := second.recorded("tunnel.startOnLaunch"); len(calls) != 1 {
		t.Fatalf("start marked calls after reconnect = %d, want 1", len(calls))
	}
}

func TestShutdownCancelsReconnect(t *testing.T) {
	first := newFakeDaemon("first.toml")
	started := make(chan struct{})
	var attempts atomic.Int32
	s := newService(func(ctx context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, fakeAuth{}, &secret.Memory{})
	first.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not start")
	}
	done := make(chan struct{})
	go func() { s.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel reconnect")
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
