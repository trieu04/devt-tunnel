package engine

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
)

func TestSettingsIOCannotHoldLifecycle(t *testing.T) {
	for _, method := range []string{"shortcut", "updates", "tool", "agent"} {
		t.Run(method, func(t *testing.T) {
			e, _, _ := newTestEngine(t, model.Config{})
			initial := settings.Settings{Shortcut: "old", Tools: map[string]string{"ssh": "/disposable/old-ssh"}}
			e.settings = initial
			e.tools.SetOverrides(initial.Tools)
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var saves atomic.Int32
			e.saveSettings = func(string, settings.Settings) error {
				saves.Add(1)
				close(entered)
				<-release
				return nil
			}
			finished := make(chan error, 1)
			go func() {
				switch method {
				case "agent":
					finished <- e.SetAgentSettingsFor(api.Actor{Kind: "gui", Name: "person"}, settings.DefaultAgent())
				case "shortcut":
					finished <- e.SetShortcut("new")
				case "updates":
					finished <- e.SetUpdateChecks(false)
				case "tool":
					_, err := e.SetToolPath("ssh", "")
					finished <- err
				}
			}()
			receive(t, entered)
			unsubscribed := make(chan struct{})
			cancel := e.Subscribe(func(api.Event) {})
			go func() { cancel(); close(unsubscribed) }()
			select {
			case <-unsubscribed:
			case <-time.After(time.Second):
				t.Fatal("unsubscribe waited for settings write")
			}
			queued := make(chan error, 1)
			go func() { queued <- e.SetShortcut("queued") }()
			shutdown := make(chan struct{})
			go func() { e.Shutdown(); close(shutdown) }()
			select {
			case <-shutdown:
			case <-time.After(time.Second):
				t.Fatal("shutdown waited for settings write")
			}
			unblock()
			for _, result := range []chan error{finished, queued} {
				if err := receive(t, result); !errors.Is(err, context.Canceled) {
					t.Fatalf("settings continued: %v", err)
				}
			}
			if saves.Load() != 1 {
				t.Fatalf("queued save issued after shutdown: %d", saves.Load())
			}
			if !reflect.DeepEqual(e.settings, initial) {
				t.Fatalf("applied settings after shutdown: %+v", e.settings)
			}
			path, _, _ := e.tools.Locate("ssh")
			if path != initial.Tools["ssh"] {
				t.Fatalf("applied tool override after shutdown: %q", path)
			}
		})
	}
}

func TestConcurrentSettingsSnapshotsPreserveUpdates(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	e.saveSettings = func(path string, next settings.Settings) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return settings.Save(path, next)
	}
	first := make(chan error, 1)
	go func() { first <- e.SetShortcut("new") }()
	receive(t, entered)
	second := make(chan error, 1)
	go func() { second <- e.SetUpdateChecks(false) }()
	unblock()
	for _, result := range []chan error{first, second} {
		if err := receive(t, result); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := settings.Load(e.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Shortcut != "new" || saved.CheckUpdates == nil || *saved.CheckUpdates || !reflect.DeepEqual(saved, e.settings) {
		t.Fatalf("lost settings update: %+v / %+v", saved, e.settings)
	}
}

func TestAgentSettingsSnapshotsDoNotShareAllowLists(t *testing.T) {
	e, _, _ := newTestEngine(t, model.Config{})
	person := api.Actor{Kind: "gui", Name: "person"}
	policy := settings.DefaultAgent()
	policy.AllowTunnels = []string{"dev-*"}
	policy.AllowSSHHosts = []string{"staging"}
	if err := e.SetAgentSettingsFor(person, policy); err != nil {
		t.Fatal(err)
	}
	policy.AllowTunnels[0] = "*"
	policy.AllowSSHHosts[0] = "production"
	snapshot := e.AgentSettings()
	if snapshot.AllowTunnels[0] != "dev-*" || snapshot.AllowSSHHosts[0] != "staging" {
		t.Fatal("setter retained caller's allow-list storage")
	}
	snapshot.AllowTunnels[0] = "*"
	snapshot.AllowSSHHosts[0] = "production"
	next := e.AgentSettings()
	if next.AllowTunnels[0] != "dev-*" || next.AllowSSHHosts[0] != "staging" {
		t.Fatal("getter exposed mutable allow-list storage")
	}
}
