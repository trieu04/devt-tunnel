package engine

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
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func TestDaemonRestartFinishesPersistedLastClose(t *testing.T) {
	stops := 0
	e, _, _ := ownedEngine(t, func(o *Options) {
		st := corestate.Runtime{Version: 1, Ephemeral: []model.Tunnel{}, Leases: []model.Lease{}, Owners: map[string]corestate.RuntimeOwner{"service": {LeaseStarted: true}}}
		if err := (corestate.RuntimeStore{Path: filepath.Join(o.StateDir, "runtime.json")}).Save(st); err != nil {
			t.Fatal(err)
		}
		if err := (&supervisor.FileRecords{Path: filepath.Join(o.StateDir, "processes.json")}).Add(supervisor.Record{TunnelID: "service", PID: 12345, Identity: "survivor"}); err != nil {
			t.Fatal(err)
		}
		o.ProcessIdentity = func(int) (string, error) { return "survivor", nil }
		o.StopProcess = func(int, string, time.Duration) error { stops++; return nil }
	}, savedCommand())
	if stops != 1 || e.hasOrphan("service") || e.Active() {
		t.Fatalf("last-close recovery: stops=%d orphans=%+v active=%v", stops, e.Overview().Orphans, e.Active())
	}
}

func TestDaemonRestartRepairsPromotionIntent(t *testing.T) {
	cfg := savedCommand()
	cfg.Tunnels[0].ID = "promoted"
	e, r, _ := ownedEngine(t, func(o *Options) {
		tunnel := commandTunnel()
		tunnel.ID = "tmp-old"
		now := o.Clock.Now()
		st := corestate.Runtime{Version: 1, Ephemeral: []model.Tunnel{tunnel}, Leases: []model.Lease{{ID: "ls_old", TunnelID: tunnel.ID, Holder: api.Actor{Kind: "cli", Name: "adapter"}, Session: "token", ExpiresAt: now.Add(time.Hour), CreatedAt: now, StartedIt: true}}, Owners: map[string]corestate.RuntimeOwner{tunnel.ID: {LeaseStarted: true}}, Promotions: map[string]string{tunnel.ID: "promoted"}}
		if err := (corestate.RuntimeStore{Path: filepath.Join(o.StateDir, "runtime.json")}).Save(st); err != nil {
			t.Fatal(err)
		}
		if err := (&supervisor.FileRecords{Path: filepath.Join(o.StateDir, "processes.json")}).Add(supervisor.Record{TunnelID: tunnel.ID, PID: 12345, Identity: "survivor"}); err != nil {
			t.Fatal(err)
		}
		o.ProcessIdentity = func(int) (string, error) { return "survivor", nil }
		o.StopProcess = func(int, string, time.Duration) error { return nil }
	}, cfg)
	st, err := e.runtimeStore.Load()
	if err != nil || len(st.Ephemeral) != 0 || len(st.Promotions) != 0 || st.Leases[0].TunnelID != "promoted" {
		t.Fatalf("recovery=%+v %v", st, err)
	}
	if !e.hasOrphan("promoted") || e.hasOrphan("tmp-old") {
		t.Fatalf("orphans=%+v", e.Overview().Orphans)
	}
	if len(e.manager.Config().Tunnels) != 1 || e.manager.Config().Tunnels[0].ID != "promoted" {
		t.Fatalf("definitions=%+v", e.manager.Config())
	}
	select {
	case <-r.started:
		t.Fatal("promotion recovery spawned a replacement")
	default:
	}
}

func TestEphemeralNamespaceAndMergedValidation(t *testing.T) {
	cfg := model.Config{Profiles: []model.Profile{{ID: "p", Name: "Profile"}}}
	e, _, _ := ownedEngine(t, nil, cfg)
	a, s := session(t, e, "agent")
	tunnel := commandTunnel()
	tunnel.ID = "saved-id"
	if _, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel}); err == nil {
		t.Fatal("accepted saved namespace for ephemeral")
	}
	tunnel.ID = "tmp-one"
	tunnel.Profile = "p"
	result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel})
	if err != nil {
		t.Fatal(err)
	}
	readyOwned(t, e, result.ID)
	if _, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel}); err == nil {
		t.Fatal("accepted duplicate ephemeral id")
	}
	if err := e.DeleteProfile("p"); err == nil {
		t.Fatal("saved write bypassed merged validation")
	}
	disk, err := config.Load(e.configPath)
	if err != nil || len(disk.Profiles) != 1 {
		t.Fatalf("failed validation changed disk: %+v %v", disk, err)
	}
	if _, err := e.PromoteTunnel(api.PromoteTunnelParams{ID: result.ID, SavedID: "tmp-still-temporary"}); err == nil {
		t.Fatal("promotion accepted reserved saved id")
	}
	saved := commandTunnel()
	saved.Name = "tmp-service"
	id, err := e.SaveTunnel(saved, false)
	if err != nil || strings.HasPrefix(id, "tmp-") {
		t.Fatalf("generated saved id=%q %v", id, err)
	}
}

func TestActiveIncludesLeasesAndDeferredEphemeralWithoutProcesses(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved lease", true: "ephemeral lease"}[ephemeral], func(t *testing.T) {
			cfg := savedCommand()
			cfg.Tunnels[0].Command.SecretEnvs = map[string]string{"TOKEN": "missing"}
			e, r, c := ownedEngine(t, nil, cfg)
			a, s := session(t, e, "agent")
			id := "service"
			if ephemeral {
				tunnel := cfg.Tunnels[0]
				tunnel.ID = ""
				result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: tunnel})
				if err != nil {
					t.Fatal(err)
				}
				id = result.ID
			} else {
				openTestLease(t, e, a, s)
			}
			if err := e.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			if e.manager.Active() || !e.Active() || !e.known(id) {
				t.Fatalf("lease did not protect idle/deferred start: active=%v statuses=%+v", e.Active(), e.Statuses())
			}
			select {
			case <-r.started:
				t.Fatal("missing-secret tunnel spawned")
			default:
			}
			c.Advance(DefaultLeaseTTL)
			if err := e.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			if e.Active() {
				t.Fatal("expired deferred lease prevented idle exit")
			}
			if ephemeral && e.known(id) {
				t.Fatal("expired ephemeral definition retained")
			}
		})
	}
}

func TestReconnectAfterGraceCannotResurrectLease(t *testing.T) {
	e, _, c := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "adapter")
	l := openTestLease(t, e, a, s)
	readyOwned(t, e, "service")
	if err := e.DisconnectSession(a, s); err != nil {
		t.Fatal(err)
	}
	c.Advance(DisconnectGrace)
	if _, err := e.ConnectSession(a, s); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RenewLease(a, s, api.RenewLeaseParams{ID: l.ID}); err == nil {
		t.Fatal("late reconnect renewed expired grace")
	}
	if err := e.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", false)
}

func TestCanceledConnectionCannotStartQueuedLease(t *testing.T) {
	e, r, _ := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "adapter")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.OpenLeaseContext(ctx, a, s, api.OpenLeaseParams{ID: "service"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("open canceled=%v", err)
	}
	if _, err := e.RunEphemeralContext(ctx, a, s, api.RunEphemeralParams{Tunnel: commandTunnel()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run canceled=%v", err)
	}
	select {
	case <-r.started:
		t.Fatal("canceled connection spawned")
	default:
	}
}
