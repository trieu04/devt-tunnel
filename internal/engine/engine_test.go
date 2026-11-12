package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type fakeProcess struct {
	exit chan error
	once sync.Once
}

func (p *fakeProcess) PID() int         { return 100 }
func (p *fakeProcess) Identity() string { return "fake-100" }
func (p *fakeProcess) Wait() error      { return <-p.exit }
func (p *fakeProcess) Stop(time.Duration) error {
	p.once.Do(func() { p.exit <- nil })
	return nil
}

type fakeRunner struct {
	started   chan supervisor.SpawnRequest
	processes chan *fakeProcess
}

func (r *fakeRunner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	p := &fakeProcess{exit: make(chan error, 1)}
	r.started <- req
	r.processes <- p
	go func() { _, _ = io.WriteString(req.Output, "ready\n") }()
	return p, nil
}

func newTestEngine(t *testing.T, cfg model.Config) (*Engine, *fakeRunner, *secret.Memory) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("DTT_SOCKET", filepath.Join(root, "test.sock"))
	t.Setenv("USERPROFILE", root)
	t.Setenv("SHELL", "")
	runner := &fakeRunner{started: make(chan supervisor.SpawnRequest, 16), processes: make(chan *fakeProcess, 16)}
	store := &secret.Memory{}
	path := filepath.Join(root, "tunnels.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	e, err := New(Options{ConfigPath: path, SettingsPath: filepath.Join(root, "settings.toml"), StateDir: filepath.Join(root, "state"), Secrets: store, Runner: runner, AskpassHelper: "test-askpass"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	return e, runner, store
}

func commandTunnel() model.Tunnel {
	return model.Tunnel{Name: "Local service", Type: model.TypeCommand, Command: &model.Command{Line: "fake-service", ReadyLog: "ready"}}
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for engine event")
		var zero T
		return zero
	}
}

func TestNewDraftUsesConsistentLocalServiceURL(t *testing.T) {
	e := &Engine{}
	ngrok := e.NewDraft(model.TypeNgrok)
	cloudflared := e.NewDraft(model.TypeCloudflared)
	const want = "http://127.0.0.1:3000"
	if ngrok.Ngrok == nil || ngrok.Ngrok.Addr != want {
		t.Fatalf("ngrok address = %#v, want %q", ngrok.Ngrok, want)
	}
	if cloudflared.Cloudflared == nil || cloudflared.Cloudflared.URL != want {
		t.Fatalf("cloudflared URL = %#v, want %q", cloudflared.Cloudflared, want)
	}
}

func TestTunnelLifecycle(t *testing.T) {
	e, runner, _ := newTestEngine(t, model.Config{})
	if e.Active() {
		t.Fatal("new engine is active")
	}
	id, err := e.SaveTunnel(commandTunnel(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Overview(); len(got.Tunnels) != 1 || got.Tunnels[0].Tunnel.ID != id || got.Shortcut != "" || got.ShortcutError != "" || got.Tunnels[0].Endpoints == nil {
		t.Fatalf("overview = %#v", got)
	}
	if err := e.Start(id); err != nil {
		t.Fatal(err)
	}
	if !e.Active() {
		t.Fatal("starting engine is inactive")
	}
	receive(t, runner.started)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := e.WaitReady(ctx, id)
	if err != nil || status.State != supervisor.StateRunning {
		t.Fatalf("ready = %#v, %v", status, err)
	}
	if !e.Active() {
		t.Fatal("running engine is inactive")
	}
	_, logs := e.LogsSince(id, 0)
	if len(logs) == 0 {
		t.Fatal("runner output was not retained")
	}
	e.RecordUsed(id)
	e.Stop(id)
	if e.Active() {
		t.Fatal("stopped engine is active")
	}
	if err := e.DeleteTunnel(id); err != nil {
		t.Fatal(err)
	}
	if got := e.Overview(); len(got.Tunnels) != 0 || !got.LastUsed[id].IsZero() {
		t.Fatalf("deleted tunnel retained: %#v", got)
	}
}

func TestOverviewTimesOutWhenKeychainBlocks(t *testing.T) {
	tunnel := commandTunnel()
	tunnel.ID = "blocked-keychain"
	tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "secret"}
	e, _, _ := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
	blocked := &blockingSecrets{called: make(chan struct{}, 1), release: make(chan struct{})}
	e.secrets.Inner = blocked
	e.ForgetSecrets()
	defer close(blocked.release)

	started := time.Now()
	got := e.Overview()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("overview waited %v for keychain", elapsed)
	}
	if got.KeychainAvailable {
		t.Fatal("blocked keychain reported available")
	}
	if len(got.Tunnels) != 1 || got.Tunnels[0].Status.State != supervisor.StateKeyringUnavailable {
		t.Fatalf("tunnels = %#v", got.Tunnels)
	}
	select {
	case <-blocked.called:
	default:
		t.Fatal("overview did not probe keychain")
	}
}

func TestOutsideEditConflictAndFocus(t *testing.T) {
	e, _, store := newTestEngine(t, model.Config{})
	if _, err := e.SaveTunnel(commandTunnel(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := secret.Has(e.secrets, "external"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatalf("initial secret presence = %v", err)
	}
	if err := store.Set("external", "value"); err != nil {
		t.Fatal(err)
	}
	external := commandTunnel()
	external.ID, external.Name = "outside", "Outside edit"
	if err := config.Save(e.configPath, model.Config{Tunnels: []model.Tunnel{external}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveTunnel(commandTunnel(), false); !errors.Is(err, config.ErrConflict) {
		t.Fatalf("save after outside edit = %v", err)
	}
	e.Focus()
	if got := e.Overview().Tunnels; len(got) != 1 || got[0].Tunnel.ID != "outside" {
		t.Fatalf("focus did not reload: %#v", got)
	}
	if _, err := secret.Has(e.secrets, "external"); err != nil {
		t.Fatalf("focus did not clear presence cache: %v", err)
	}
}

func TestNewDoesNotStartMarkedTunnels(t *testing.T) {
	tunnel := commandTunnel()
	tunnel.ID, tunnel.StartOnLaunch = "marked", true
	e, runner, _ := newTestEngine(t, model.Config{Tunnels: []model.Tunnel{tunnel}})
	if e.Active() {
		t.Fatal("constructor started marked tunnel")
	}
	select {
	case <-runner.started:
		t.Fatal("constructor spawned a process")
	default:
	}
	e.StartOnLaunch()
	receive(t, runner.started)
	if !e.Active() {
		t.Fatal("GUI launch request did not start marked tunnel")
	}
}

func TestBrokerPromptQueueAndAnswer(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	added := make(chan api.PendingPrompt, 1)
	cancel := e.Subscribe(func(event api.Event) {
		if event.Type == api.EventPromptAdded {
			if !e.Active() {
				t.Error("pending prompt did not make engine active")
			}
			added <- *event.Prompt
		}
	})
	defer cancel()
	env, release := e.broker.Register("ssh-test", "")
	defer release()
	values := map[string]string{}
	for _, entry := range env {
		for i, c := range entry {
			if c == '=' {
				values[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	result := make(chan string, 1)
	go func() {
		answer, err := askpass.Ask(values[askpass.EnvEndpoint], values[askpass.EnvToken], "Continue connecting (yes/no)?")
		result <- fmt.Sprintf("%s/%v", answer, err)
	}()
	prompt := receive(t, added)
	if got := e.Overview().Prompts; len(got) != 1 || got[0] != prompt || prompt.Prompt.Kind != askpass.KindHostKey {
		t.Fatalf("prompt queue = %#v", got)
	}
	e.AnswerPrompt(prompt.ID, "yes", false)
	if got := receive(t, result); got != "yes/<nil>" {
		t.Fatalf("broker answer = %s", got)
	}
	if e.Active() || len(e.Overview().Prompts) != 0 {
		t.Fatal("answered prompt retained")
	}
}

func TestPromptCancellationAndShutdown(t *testing.T) {
	for _, mode := range []string{"answer", "context", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			e, _, _ := newTestEngine(t, model.Config{})
			added := make(chan api.PendingPrompt, 1)
			e.Subscribe(func(event api.Event) {
				if event.Type == api.EventPromptAdded {
					added <- *event.Prompt
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := e.askUser(ctx, askpass.Prompt{Text: "OTP", Kind: askpass.KindOther})
				result <- err
			}()
			prompt := receive(t, added)
			switch mode {
			case "answer":
				e.AnswerPrompt(prompt.ID, "", true)
			case "context":
				cancel()
			case "shutdown":
				e.Shutdown()
			}
			if err := receive(t, result); err == nil {
				t.Fatal("cancelled prompt returned no error")
			}
			if e.Active() || len(e.Overview().Prompts) != 0 {
				t.Fatal("cancelled prompt retained")
			}
		})
	}
}

func TestCoalescedEventsAndUnsubscribe(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	events := make(chan api.Event, 16)
	cancel := e.Subscribe(func(event api.Event) {
		_ = e.Active()
		events <- event
	})
	e.notify()
	if event := receive(t, events); event.Type != api.EventTunnelsChanged {
		t.Fatalf("initial event = %#v", event)
	}
	for i := 0; i < 10; i++ {
		e.notify()
	}
	receive(t, events)
	select {
	case event := <-events:
		t.Fatalf("burst was not coalesced: %#v", event)
	case <-time.After(2 * notifyDelay):
	}
	cancel()
	cancel()
	e.emit(api.Event{Type: api.EventTunnelsChanged})
	select {
	case event := <-events:
		t.Fatalf("event after unsubscribe: %#v", event)
	default:
	}
}

func TestSettingsAndConfigPaths(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	if err := e.SetShortcut("Ctrl+Alt+K"); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUpdateChecks(false); err != nil {
		t.Fatal(err)
	}
	current, err := settings.Load(e.settingsPath)
	if err != nil || current.Shortcut != "Ctrl+Alt+K" || current.CheckUpdates == nil || *current.CheckUpdates || e.UpdateChecksEnabled() {
		t.Fatalf("settings = %#v, %v", current, err)
	}
	if err := os.Remove(e.configPath); err != nil {
		t.Fatal(err)
	}
	path, err := e.EnsureConfigFile()
	if err != nil || path != e.configPath {
		t.Fatalf("config path = %q, %v", path, err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("config template is invalid: %v", err)
	}
}

func TestClockJumped(t *testing.T) {
	start := time.Now()
	for _, tt := range []struct {
		elapsed time.Duration
		want    bool
	}{{30 * time.Second, false}, {90 * time.Second, false}, {91 * time.Second, true}, {-time.Minute, false}} {
		if got := clockJumped(start, start.Add(tt.elapsed), 30*time.Second); got != tt.want {
			t.Errorf("clock jump %s = %t, want %t", tt.elapsed, got, tt.want)
		}
	}
}

func TestActiveDuringReconnectAndWake(t *testing.T) {
	e, runner, _ := newTestEngine(t, model.Config{})
	tunnel := commandTunnel()
	tunnel.Reconnect = true
	id, err := e.SaveTunnel(tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	process := receive(t, runner.processes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.WaitReady(ctx, id); err != nil {
		t.Fatal(err)
	}
	process.once.Do(func() { process.exit <- errors.New("connection lost") })
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for e.Overview().Tunnels[0].Status.State != supervisor.StateReconnecting {
		select {
		case <-deadline:
			t.Fatal("tunnel did not enter reconnect backoff")
		case <-ticker.C:
		}
	}
	if !e.Active() {
		t.Fatal("reconnecting engine is inactive")
	}
	e.Wake()
	receive(t, runner.processes)
	if _, err := e.WaitReady(ctx, id); err != nil {
		t.Fatalf("wake did not retry: %v", err)
	}
	e.Stop(id)
}

func TestMissingSecretRetriesWithMemoryStore(t *testing.T) {
	e, runner, store := newTestEngine(t, model.Config{})
	tunnel := commandTunnel()
	tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "test-token"}
	id, err := e.SaveTunnel(tunnel, true)
	if err != nil {
		t.Fatal(err)
	}
	if e.Active() {
		t.Fatal("missing-secret tunnel should wait without a process")
	}
	if err := e.SetSecret("test-token", "test-value", secret.KindEnv); err != nil {
		t.Fatal(err)
	}
	request := receive(t, runner.started)
	found := false
	for _, entry := range request.Env {
		found = found || entry == "TOKEN=test-value"
	}
	if value, err := store.Get("test-token"); err != nil || value != "test-value" || !found {
		t.Fatalf("memory secret was not stored and injected: %v", err)
	}
	if secrets := e.ListSecrets(); len(secrets) != 1 || secrets[0].State != "present" || len(secrets[0].UsedBy) != 1 {
		t.Fatalf("vault = %#v", secrets)
	}
	if err := e.RenameSecret("test-token", "test-token"); err != nil {
		t.Fatal(err)
	}
	if value, err := store.Get("test-token"); err != nil || value != "test-value" {
		t.Fatalf("rename to the same name lost the secret: %q %v", value, err)
	}
	if err := e.RenameSecret("test-token", "renamed-token"); err != nil {
		t.Fatal(err)
	}
	if got := e.Overview().Tunnels[0].Tunnel.Command.SecretEnvs["TOKEN"]; got != "renamed-token" {
		t.Fatalf("renamed reference = %q", got)
	}
	e.Stop(id)
	if err := e.DeleteSecret("renamed-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("renamed-token"); !errors.Is(err, secret.ErrNotFound) {
		t.Fatalf("deleted secret = %v", err)
	}
}
