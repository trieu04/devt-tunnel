package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type fakeAuth struct{}

func (fakeAuth) Available() bool                       { return false }
func (fakeAuth) Confirm(context.Context, string) error { return errors.New("unavailable") }

type countingHTTPClient struct{ calls atomic.Int32 }

func (c *countingHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return nil, errors.New("unexpected HTTP request")
}

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
	logs     []api.LogsResult
	readLogs []string
	lastLogs api.LogsResult
	pid      int
}

var nextFakePID atomic.Int32

type failingShutdownClient struct {
	*fakeDaemon
	fail error
}

type blockingClient struct {
	*fakeDaemon
	block       atomic.Bool
	blockMethod string
	started     chan struct{}
	release     chan struct{}
}

func (c *blockingClient) Call(ctx context.Context, method string, params, result any) error {
	err := c.fakeDaemon.Call(ctx, method, params, result)
	if err == nil && method == c.blockMethod && c.block.Load() {
		select {
		case <-c.started:
		default:
			close(c.started)
		}
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (c *failingShutdownClient) Call(ctx context.Context, method string, params, result any) error {
	if method == api.DaemonShutdown {
		c.Close()
		return c.fail
	}
	return c.fakeDaemon.Call(ctx, method, params, result)
}

func newFakeDaemon(path string) *fakeDaemon {
	return &fakeDaemon{done: make(chan struct{}), overview: api.Overview{ConfigPath: path}, settings: api.SettingsResult{Shortcut: "Alt+Space", UpdateChecks: true}, pid: int(nextFakePID.Add(1))}
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
	case api.EventsSubscribe, "tunnel.start", "tunnel.stop", "tunnel.startOnLaunch", "settings.setUpdateChecks", "settings.setAgent", "lease.revoke":
		if out, ok := result.(*api.Empty); ok {
			*out = api.Empty{}
		}
	case api.DaemonShutdown:
		if expected := params.(api.ShutdownParams).ExpectedPID; expected != 0 && expected != f.pid {
			return api.NewError(api.Conflict, "daemon identity changed")
		}
		if out, ok := result.(*api.Empty); ok {
			*out = api.Empty{}
		}
	case "config.overview":
		*(result.(*api.Overview)) = f.overview
	case "logs.read":
		*(result.(*[]string)) = append([]string(nil), f.readLogs...)
	case "approval.list":
		*(result.(*[]api.Approval)) = []api.Approval{{ID: "ap_test", Code: "A1B2C3D4", Choices: []string{"once", "session"}, Status: "pending"}}
	case "approval.decide":
		p := params.(api.DecideApprovalParams)
		*(result.(*api.Approval)) = api.Approval{ID: p.ID, Status: "approved", Scope: p.Scope}
	case "settings.getAgent":
		*(result.(*settings.Agent)) = settings.DefaultAgent()
	case "lease.list":
		*(result.(*[]api.Lease)) = []api.Lease{{ID: "ls_test", TunnelID: "tmp_test"}}
	case "tunnel.promote":
		*(result.(*string)) = "saved-test"
	case "settings.get":
		*(result.(*api.SettingsResult)) = f.settings
	case "logs.since":
		if len(f.logs) > 0 {
			f.lastLogs = f.logs[0]
			f.logs = f.logs[1:]
		} else {
			f.lastLogs.Lines = nil
		}
		*(result.(*api.LogsResult)) = f.lastLogs
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

func (f *fakeDaemon) Hello() api.HelloResult { return api.HelloResult{PID: f.pid} }
func (f *fakeDaemon) Done() <-chan struct{}  { return f.done }
func (f *fakeDaemon) Close() error           { f.closed.Do(func() { close(f.done) }); return nil }

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

	events := make(chan api.Event, 3)
	s.mu.Lock()
	s.eventSink = func(event api.Event) { events <- event }
	s.mu.Unlock()
	prompt := api.PendingPrompt{ID: "prompt-1"}
	daemon.emit(api.Event{Type: api.EventPromptAdded, Prompt: &prompt})
	daemon.emit(api.Event{Type: api.EventTunnelsChanged})
	daemon.emit(api.Event{Type: api.EventApprovalAdded, ApprovalID: "ap_test"})
	for _, want := range []string{api.EventPromptAdded, api.EventTunnelsChanged, api.EventApprovalAdded} {
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

func TestOverviewDoesNotCacheOldGenerationResult(t *testing.T) {
	first := &blockingClient{fakeDaemon: newFakeDaemon("first.toml"), blockMethod: "config.overview", started: make(chan struct{}), release: make(chan struct{})}
	second := newFakeDaemon("second.toml")
	var attempts atomic.Int32
	s := newService(func(context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	first.block.Store(true)
	result := make(chan api.Overview, 1)
	go func() { result <- s.Overview() }()
	select {
	case <-first.started:
	case <-time.After(time.Second):
		t.Fatal("old-generation overview did not start")
	}
	first.Close()
	eventually(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.client == second
	})
	close(first.release)
	select {
	case got := <-result:
		if got.ConfigPath != "second.toml" || got.ConfigError != "" {
			t.Fatalf("overview = %#v, want healthy reconnected cache", got)
		}
	case <-time.After(time.Second):
		t.Fatal("old-generation overview did not return")
	}
	s.mu.Lock()
	cached := s.overview.ConfigPath
	s.mu.Unlock()
	if cached != "second.toml" {
		t.Fatalf("cached overview path = %q, want second.toml", cached)
	}
}

func TestReadDoesNotReturnOldGenerationResult(t *testing.T) {
	firstDaemon := newFakeDaemon("first.toml")
	firstDaemon.readLogs = []string{"old generation"}
	first := &blockingClient{fakeDaemon: firstDaemon, blockMethod: "logs.read", started: make(chan struct{}), release: make(chan struct{})}
	second := newFakeDaemon("second.toml")
	var attempts atomic.Int32
	s := newService(func(context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	first.block.Store(true)
	result := make(chan []string, 1)
	go func() { result <- s.Logs("db") }()
	select {
	case <-first.started:
	case <-time.After(time.Second):
		t.Fatal("old-generation read did not start")
	}
	first.Close()
	eventually(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.client == second
	})
	close(first.release)
	select {
	case got := <-result:
		if len(got) != 0 {
			t.Fatalf("logs = %#v, want stale result rejected", got)
		}
	case <-time.After(time.Second):
		t.Fatal("old-generation read did not return")
	}
}

func TestOldGenerationEventsAreIgnored(t *testing.T) {
	first := newFakeDaemon("first.toml")
	second := newFakeDaemon("second.toml")
	var attempts atomic.Int32
	s := newService(func(context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	events := make(chan api.Event, 2)
	s.mu.Lock()
	s.eventSink = func(event api.Event) { events <- event }
	s.mu.Unlock()
	first.Close()
	eventually(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.client == second
	})
	first.emit(api.Event{Type: api.EventPromptAdded, Prompt: &api.PendingPrompt{ID: "old"}})
	second.emit(api.Event{Type: api.EventPromptAdded, Prompt: &api.PendingPrompt{ID: "new"}})
	select {
	case got := <-events:
		if got.Prompt == nil || got.Prompt.ID != "new" {
			t.Fatalf("event = %#v, want new generation prompt", got)
		}
	case <-time.After(time.Second):
		t.Fatal("new-generation event was not delivered")
	}
	select {
	case got := <-events:
		t.Fatalf("unexpected old-generation event: %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestShutdownCancelsReconnect(t *testing.T) {
	first := newFakeDaemon("first.toml")
	started := make(chan struct{})
	var attempts atomic.Int32
	connect := func(ctx context.Context) (daemonClient, error) {
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
	}
	s := newServiceWithConnectors(connect, func(ctx context.Context) (daemonClient, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, fakeAuth{}, &secret.Memory{})
	s.shutdownLimit = 50 * time.Millisecond
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
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown did not cancel reconnect")
	}
}

func TestShutdownUsesManagementConnectionDuringReconnect(t *testing.T) {
	first := newFakeDaemon("first.toml")
	management := newFakeDaemon("management.toml")
	reconnecting := make(chan struct{})
	var attempts atomic.Int32
	connect := func(ctx context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		select {
		case <-reconnecting:
		default:
			close(reconnecting)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var managementConnections atomic.Int32
	s := newServiceWithConnectors(connect, func(context.Context) (daemonClient, error) {
		managementConnections.Add(1)
		return management, nil
	}, fakeAuth{}, &secret.Memory{})
	first.Close()
	select {
	case <-reconnecting:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not start")
	}
	s.Shutdown()
	if got := managementConnections.Load(); got != 1 {
		t.Fatalf("management connections = %d, want 1", got)
	}
	if calls := management.recorded(api.DaemonShutdown); len(calls) != 1 {
		t.Fatalf("daemon shutdown calls = %d, want 1", len(calls))
	}
}

func TestShutdownRetriesTransportFailureOnManagementConnection(t *testing.T) {
	installed := &failingShutdownClient{fakeDaemon: newFakeDaemon("installed.toml"), fail: io.EOF}
	management := newFakeDaemon("management.toml")
	management.pid = installed.pid
	var managementConnections atomic.Int32
	s := newServiceWithConnectors(func(context.Context) (daemonClient, error) { return installed, nil }, func(context.Context) (daemonClient, error) {
		managementConnections.Add(1)
		return management, nil
	}, fakeAuth{}, &secret.Memory{})
	s.Shutdown()
	if got := managementConnections.Load(); got != 1 {
		t.Fatalf("management connections = %d, want 1", got)
	}
	if calls := management.recorded(api.DaemonShutdown); len(calls) != 1 {
		t.Fatalf("management shutdown calls = %d, want 1", len(calls))
	}
}

func TestShutdownRetryDoesNotStopReplacementDaemon(t *testing.T) {
	installed := &failingShutdownClient{fakeDaemon: newFakeDaemon("installed.toml"), fail: io.EOF}
	replacement := newFakeDaemon("replacement.toml")
	s := newServiceWithConnectors(func(context.Context) (daemonClient, error) { return installed, nil }, func(context.Context) (daemonClient, error) {
		return replacement, nil
	}, fakeAuth{}, &secret.Memory{})
	s.Shutdown()
	calls := replacement.recorded(api.DaemonShutdown)
	if len(calls) != 1 {
		t.Fatalf("replacement shutdown calls = %d, want 1 fenced attempt", len(calls))
	}
	params := calls[0].params.(api.ShutdownParams)
	if params.ExpectedPID != installed.pid || params.ExpectedPID == replacement.pid {
		t.Fatalf("shutdown expected PID = %d, installed = %d, replacement = %d", params.ExpectedPID, installed.pid, replacement.pid)
	}
}

func TestShutdownDoesNotRetryRPCFailure(t *testing.T) {
	installed := &failingShutdownClient{fakeDaemon: newFakeDaemon("installed.toml"), fail: api.NewError(api.Conflict, "active")}
	var managementConnections atomic.Int32
	s := newServiceWithConnectors(func(context.Context) (daemonClient, error) { return installed, nil }, func(context.Context) (daemonClient, error) {
		managementConnections.Add(1)
		return newFakeDaemon("management.toml"), nil
	}, fakeAuth{}, &secret.Memory{})
	s.Shutdown()
	if got := managementConnections.Load(); got != 0 {
		t.Fatalf("management connections = %d, want 0", got)
	}
}

func TestAutomaticUpdateCheckWaitsForSettings(t *testing.T) {
	daemon := newFakeDaemon("settings.toml")
	daemon.settings.UpdateChecks = false
	var available atomic.Bool
	connect := func(context.Context) (daemonClient, error) {
		if !available.Load() {
			return nil, errors.New("daemon unavailable")
		}
		return daemon, nil
	}
	s := newServiceWithConnectors(connect, func(context.Context) (daemonClient, error) {
		return nil, errors.New("daemon unavailable")
	}, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	httpClient := &countingHTTPClient{}
	s.mu.Lock()
	s.updateClient = httpClient
	settingsChanged := make(chan struct{}, 1)
	s.settingsSink = func() { settingsChanged <- struct{}{} }
	s.mu.Unlock()
	if _, err := s.CheckForUpdate(false); err != nil {
		t.Fatal(err)
	}
	if got := httpClient.calls.Load(); got != 0 {
		t.Fatalf("HTTP calls before settings = %d, want 0", got)
	}
	if _, err := s.CheckForUpdate(true); err == nil {
		t.Fatal("forced update check unexpectedly succeeded")
	}
	if got := httpClient.calls.Swap(0); got != 1 {
		t.Fatalf("forced HTTP calls = %d, want 1", got)
	}
	available.Store(true)
	select {
	case <-settingsChanged:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not emit settings changed")
	}
	eventually(t, 3*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.settingsLoaded
	})
	if s.UpdateChecksEnabled() {
		t.Fatal("disabled persisted update preference was not loaded")
	}
	if _, err := s.CheckForUpdate(false); err != nil {
		t.Fatal(err)
	}
	if got := httpClient.calls.Load(); got != 0 {
		t.Fatalf("HTTP calls with checks disabled = %d, want 0", got)
	}
}

func TestConfirmedDefaultEnablesUpdateChecks(t *testing.T) {
	daemon := newFakeDaemon("settings.toml")
	s := newService(func(context.Context) (daemonClient, error) { return daemon, nil }, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	if !s.UpdateChecksEnabled() {
		t.Fatal("confirmed default update preference is disabled")
	}
}

func TestSettingsMutationEmitsChanged(t *testing.T) {
	daemon := newFakeDaemon("settings.toml")
	s := newService(func(context.Context) (daemonClient, error) { return daemon, nil }, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	changed := make(chan struct{}, 1)
	s.mu.Lock()
	s.settingsSink = func() { changed <- struct{}{} }
	s.mu.Unlock()
	if err := s.SetUpdateChecks(false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("settings mutation did not emit changed")
	}
	select {
	case <-changed:
		t.Fatal("settings mutation emitted changed more than once")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWatchLogsResumesAfterReconnect(t *testing.T) {
	first := newFakeDaemon("first.toml")
	second := newFakeDaemon("second.toml")
	second.logs = []api.LogsResult{{Sequence: 2, Lines: []supervisor.LogLine{{Seq: 1, Line: "daemon ready"}, {Seq: 2, Line: "tunnel ready"}}}}
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
	appended := make(chan api.LogAppend, 1)
	s.mu.Lock()
	s.logSink = func(lines api.LogAppend) { appended <- lines }
	s.mu.Unlock()
	first.Close()
	if initial := s.WatchLogs("db"); len(initial) != 0 {
		t.Fatalf("initial logs = %#v, want empty", initial)
	}
	reconnect.Store(true)
	select {
	case got := <-appended:
		if got.ID != "db" || len(got.Lines) != 2 || got.Lines[0].Line != "daemon ready" {
			t.Fatalf("appended logs = %#v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("logs did not resume after reconnect")
	}
}

func TestWatchLogsRefetchesAfterSequenceReset(t *testing.T) {
	daemon := newFakeDaemon("logs.toml")
	daemon.logs = []api.LogsResult{
		{Sequence: 5, Lines: []supervisor.LogLine{{Seq: 5, Line: "old daemon"}}},
		{Sequence: 2},
		{Sequence: 2, Lines: []supervisor.LogLine{{Seq: 1, Line: "new daemon"}, {Seq: 2, Line: "new tunnel"}}},
	}
	s := newService(func(context.Context) (daemonClient, error) { return daemon, nil }, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	appended := make(chan api.LogAppend, 2)
	s.mu.Lock()
	s.logSink = func(lines api.LogAppend) { appended <- lines }
	s.mu.Unlock()
	initial := s.WatchLogs("db")
	if len(initial) != 1 || initial[0].Seq != 5 {
		t.Fatalf("initial logs = %#v", initial)
	}
	select {
	case got := <-appended:
		if !got.Replace || len(got.Lines) != 2 || got.Lines[0].Seq != 1 || got.Lines[1].Seq != 2 {
			t.Fatalf("reset logs = %#v", got.Lines)
		}
	case <-time.After(time.Second):
		t.Fatal("reset logs were not refetched")
	}
	calls := daemon.recorded("logs.since")
	if len(calls) < 3 {
		t.Fatalf("logs.since calls = %d, want at least 3", len(calls))
	}
	for i, want := range []uint64{0, 5, 0} {
		if got := calls[i].params.(api.LogsParams).Sequence; got != want {
			t.Fatalf("logs.since call %d sequence = %d, want %d", i, got, want)
		}
	}
	select {
	case duplicate := <-appended:
		t.Fatalf("duplicate logs emitted: %#v", duplicate)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestWatchLogsRefetchesAfterGenerationChangeWithHigherSequence(t *testing.T) {
	first := newFakeDaemon("first.toml")
	first.logs = []api.LogsResult{{Sequence: 5, Lines: []supervisor.LogLine{{Seq: 5, Line: "old"}}}}
	second := newFakeDaemon("second.toml")
	newLines := make([]supervisor.LogLine, 7)
	for i := range newLines {
		newLines[i] = supervisor.LogLine{Seq: uint64(i + 1), Line: fmt.Sprintf("new-%d", i+1)}
	}
	second.logs = []api.LogsResult{{Sequence: 7, Lines: newLines}}
	var attempts atomic.Int32
	var reconnect atomic.Bool
	s := newService(func(context.Context) (daemonClient, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		if !reconnect.Load() {
			return nil, errors.New("daemon unavailable")
		}
		return second, nil
	}, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	appended := make(chan api.LogAppend, 2)
	s.mu.Lock()
	s.logSink = func(lines api.LogAppend) { appended <- lines }
	s.mu.Unlock()
	initial := s.WatchLogs("db")
	if len(initial) != 1 || initial[0].Seq != 5 {
		t.Fatalf("initial logs = %#v", initial)
	}
	first.Close()
	reconnect.Store(true)
	select {
	case got := <-appended:
		if !got.Replace || len(got.Lines) != 7 || got.Lines[0].Seq != 1 || got.Lines[6].Seq != 7 {
			t.Fatalf("new daemon logs = %#v", got.Lines)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("new daemon logs were not refetched")
	}
	select {
	case duplicate := <-appended:
		t.Fatalf("duplicate logs emitted: %#v", duplicate)
	case <-time.After(250 * time.Millisecond):
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

func TestServiceForwardsAgentControls(t *testing.T) {
	daemon := newFakeDaemon("fake.toml")
	s := newService(func(context.Context) (daemonClient, error) { return daemon, nil }, fakeAuth{}, &secret.Memory{})
	t.Cleanup(s.Shutdown)
	approvals, err := s.ListApprovals()
	if err != nil || len(approvals) != 1 || approvals[0].Code != "A1B2C3D4" {
		t.Fatalf("approval listing: %+v / %v", approvals, err)
	}
	decision := api.DecideApprovalParams{ID: approvals[0].ID, Code: approvals[0].Code, Decision: "approve", Scope: "session"}
	if _, err := s.DecideApproval(decision); err != nil {
		t.Fatal(err)
	}
	if calls := daemon.recorded("approval.decide"); len(calls) != 1 || !reflect.DeepEqual(calls[0].params, decision) {
		t.Fatalf("decision changed: %+v", calls)
	}
	agent, err := s.AgentSettings()
	if err != nil || agent.Public != "ask" {
		t.Fatalf("policy: %+v / %v", agent, err)
	}
	agent.Enabled = false
	if err := s.SetAgentSettings(agent); err != nil {
		t.Fatal(err)
	}
	if calls := daemon.recorded("settings.setAgent"); len(calls) != 1 || !reflect.DeepEqual(calls[0].params, api.AgentSettingsParams{Agent: agent}) {
		t.Fatalf("policy changed: %+v", calls)
	}
	leases, err := s.ListLeases()
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases: %+v / %v", leases, err)
	}
	if err := s.RevokeLease(leases[0].ID); err != nil {
		t.Fatal(err)
	}
	if calls := daemon.recorded("lease.revoke"); len(calls) != 1 || !reflect.DeepEqual(calls[0].params, api.IDParams{ID: "ls_test"}) {
		t.Fatalf("revoke: %+v", calls)
	}
	id, err := s.PromoteTunnel("tmp_test", "Saved test")
	if err != nil || id != "saved-test" {
		t.Fatalf("promote: %s / %v", id, err)
	}
	if calls := daemon.recorded("tunnel.promote"); len(calls) != 1 || !reflect.DeepEqual(calls[0].params, api.PromoteTunnelParams{ID: "tmp_test", Name: "Saved test"}) {
		t.Fatalf("promote: %+v", calls)
	}
}
