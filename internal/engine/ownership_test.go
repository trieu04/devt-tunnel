package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type testClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []clockTimer
}
type clockTimer struct {
	at time.Time
	ch chan time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, clockTimer{c.now.Add(d), ch})
	return ch
}
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.timers[:0]
	for _, timer := range c.timers {
		if !c.now.Before(timer.at) {
			timer.ch <- c.now
		} else {
			kept = append(kept, timer)
		}
	}
	c.timers = kept
}

func ownedEngine(t *testing.T, prepare func(*Options), cfg model.Config) (*Engine, *fakeRunner, *testClock) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("SHELL", "")
	t.Setenv("DTT_SOCKET", filepath.Join(root, "test.sock"))
	r := &fakeRunner{started: make(chan supervisor.SpawnRequest, 32), processes: make(chan *fakeProcess, 32)}
	c := &testClock{now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	opts := Options{ConfigPath: filepath.Join(root, "tunnels.toml"), SettingsPath: filepath.Join(root, "settings.toml"), StateDir: filepath.Join(root, "state"), Secrets: &secret.Memory{}, Runner: r, AskpassHelper: "unused", Clock: c}
	if err := config.Save(opts.ConfigPath, cfg); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(&opts)
	}
	e, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Shutdown)
	return e, r, c
}
func savedCommand() model.Config {
	t := commandTunnel()
	t.ID = "service"
	return model.Config{Tunnels: []model.Tunnel{t}}
}

// session uses a person actor so lifecycle and allocation tests exercise ownership,
// connection grace, and port retries independently of MCP approval policy.
func session(t *testing.T, e *Engine, name string) (api.Actor, string) {
	t.Helper()
	a := api.Actor{Kind: "cli", Name: name}
	s, err := e.ConnectSession(a, "")
	if err != nil {
		t.Fatal(err)
	}
	return a, s
}
func openTestLease(t *testing.T, e *Engine, a api.Actor, s string) api.Lease {
	t.Helper()
	l, err := e.OpenLease(a, s, api.OpenLeaseParams{ID: "service"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func readyOwned(t *testing.T, e *Engine, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.WaitReady(ctx, id); err != nil {
		t.Fatal(err)
	}
}
func requireRunning(t *testing.T, e *Engine, id string, want bool) {
	t.Helper()
	if got := e.running(id); got != want {
		t.Fatalf("running(%s) = %v want %v: %+v", id, got, want, e.Statuses())
	}
}
func requireRevoked(t *testing.T, err error) {
	t.Helper()
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.LeaseRevoked {
		t.Fatalf("expected lease_revoked, got %v", err)
	}
}

// Phase 19.2 case 1.
func TestLeasesTwoHoldersOneCloses(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "first")
	b, u := session(t, e, "second")
	first := openTestLease(t, e, a, s)
	readyOwned(t, e, "service")
	second := openTestLease(t, e, b, u)
	if !first.StartedIt || second.StartedIt {
		t.Fatalf("startedIt = %v/%v", first.StartedIt, second.StartedIt)
	}
	if err := e.CloseLease(a, s, first.ID); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", true)
	if got := e.owner("service"); got.Leases != 1 || got.Pinned || got.Holder == nil || got.Holder.Name != "second" || got.Expiry == nil {
		t.Fatalf("owner = %+v", got)
	}
	// StartedIt must outlive the first lease rather than disappearing with it.
	if err := e.CloseLease(b, u, second.ID); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", false)
}

// Phase 19.2 case 2.
func TestLeasesLastHolderStopsLeaseStarted(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "agent")
	l := openTestLease(t, e, a, s)
	readyOwned(t, e, "service")
	if err := e.CloseLease(a, s, l.ID); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", false)
	if e.Active() {
		t.Fatal("last close left engine active")
	}
}

// Phase 19.2 case 3: inherited/unowned running tunnel, not a person pin.
func TestLeasesAlreadyRunningStaysUp(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, savedCommand())
	if err := e.manager.Start("service"); err != nil {
		t.Fatal(err)
	}
	readyOwned(t, e, "service")
	a, s := session(t, e, "agent")
	l := openTestLease(t, e, a, s)
	if l.StartedIt {
		t.Fatal("claimed existing process start")
	}
	if err := e.CloseLease(a, s, l.ID); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", true)
}

// Phase 19.2 case 4, including a person pinning a lease-started tunnel.
func TestLeasesPersonStartPinsAgainstExpiry(t *testing.T) {
	for _, personFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(personFirst), func(t *testing.T) {
			e, _, c := ownedEngine(t, nil, savedCommand())
			a, s := session(t, e, "agent")
			if personFirst {
				if err := e.Start("service"); err != nil {
					t.Fatal(err)
				}
				readyOwned(t, e, "service")
			}
			l := openTestLease(t, e, a, s)
			readyOwned(t, e, "service")
			if err := e.Start("service"); err != nil {
				t.Fatal(err)
			}
			c.Advance(DefaultLeaseTTL)
			if err := e.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			requireRunning(t, e, "service", true)
			if got := e.owner("service"); !got.Pinned || got.Leases != 0 {
				t.Fatalf("owner=%+v lease=%+v", got, l)
			}
		})
	}
}

// Phase 19.2 case 5.
func TestLeasesPersonStopRevokesImmediately(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "agent")
	l := openTestLease(t, e, a, s)
	readyOwned(t, e, "service")
	if err := e.StopTunnel("service"); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", false)
	_, err := e.RenewLease(a, s, api.RenewLeaseParams{ID: l.ID})
	requireRevoked(t, err)
	requireRevoked(t, e.CloseLease(a, s, l.ID))
	if o := e.owner("service"); o.Pinned || o.Leases != 0 {
		t.Fatalf("stopped owner = %+v", o)
	}
	if e.Active() {
		t.Fatal("revocation tombstone counted as active lease")
	}
}

// Phase 19.2 case 6.
func TestLeasesTTLExpires(t *testing.T) {
	e, _, c := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "agent")
	l, err := e.OpenLease(a, s, api.OpenLeaseParams{ID: "service", TTL: "5m"})
	if err != nil {
		t.Fatal(err)
	}
	readyOwned(t, e, "service")
	c.Advance(5*time.Minute - time.Nanosecond)
	if err := e.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", true)
	c.Advance(time.Nanosecond)
	if err := e.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, "service", false)
	if got := e.ListLeases(a, s, true); len(got) != 0 {
		t.Fatalf("expired lease retained %+v (%+v)", got, l)
	}
}

// Phase 19.2 case 7.
func TestLeasesConnectionGraceAndReconnect(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		t.Run(fmt.Sprint(reconnect), func(t *testing.T) {
			e, _, c := ownedEngine(t, nil, savedCommand())
			a, s := session(t, e, "adapter")
			l := openTestLease(t, e, a, s)
			readyOwned(t, e, "service")
			if err := e.DisconnectSession(a, s); err != nil {
				t.Fatal(err)
			}
			c.Advance(DisconnectGrace - time.Nanosecond)
			if err := e.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			requireRunning(t, e, "service", true)
			if reconnect {
				if _, err := e.ConnectSession(a, s); err != nil {
					t.Fatal(err)
				}
			}
			c.Advance(time.Nanosecond)
			if err := e.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			requireRunning(t, e, "service", reconnect)
			if reconnect {
				if _, err := e.RenewLease(a, s, api.RenewLeaseParams{ID: l.ID}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// Phase 19.2 case 8, with fake OS identities: Unix survivor and Windows job death.
func TestLeasesDaemonRestartRecovery(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		expired, alive, pinned bool
	}{
		{"expired orphan", true, true, false}, {"surviving orphan", false, true, false}, {"OS killed child", false, false, false}, {"expired pinned orphan", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stops := 0
			e, r, c := ownedEngine(t, func(opts *Options) {
				now := opts.Clock.Now()
				expiry := now.Add(time.Hour)
				if tc.expired {
					expiry = now.Add(-time.Second)
				}
				st := corestate.Runtime{Version: 1, Ephemeral: []model.Tunnel{}, Leases: []model.Lease{{ID: "ls_old", TunnelID: "service", Holder: api.Actor{Kind: "cli", Name: "adapter"}, Session: "old-session", CreatedAt: now.Add(-time.Hour), ExpiresAt: expiry, StartedIt: true}}, Owners: map[string]corestate.RuntimeOwner{"service": {Pinned: tc.pinned, LeaseStarted: true}}}
				if err := (corestate.RuntimeStore{Path: filepath.Join(opts.StateDir, "runtime.json")}).Save(st); err != nil {
					t.Fatal(err)
				}
				records := &supervisor.FileRecords{Path: filepath.Join(opts.StateDir, "processes.json")}
				if err := records.Add(supervisor.Record{TunnelID: "service", PID: 12345, Identity: "identity"}); err != nil {
					t.Fatal(err)
				}
				opts.ProcessIdentity = func(int) (string, error) {
					if tc.alive {
						return "identity", nil
					}
					return "", errors.New("dead")
				}
				opts.StopProcess = func(int, string, time.Duration) error { stops++; return nil }
			}, savedCommand())
			wantAlive := tc.alive && (!tc.expired || tc.pinned)
			if e.hasOrphan("service") != wantAlive {
				t.Fatalf("orphan retained=%v want=%v", e.hasOrphan("service"), wantAlive)
			}
			if got := e.ListLeases(api.Actor{}, "", false); (len(got) == 0) != tc.expired {
				t.Fatalf("restored leases=%+v", got)
			}
			select {
			case <-r.started:
				t.Fatal("restart respawned a tunnel")
			default:
			}
			if !tc.expired && tc.alive {
				if _, err := e.ConnectSession(api.Actor{Kind: "cli", Name: "adapter"}, "old-session"); err != nil {
					t.Fatal(err)
				}
				c.Advance(time.Hour)
				if err := e.ExpireLeases(); err != nil {
					t.Fatal(err)
				}
				if e.hasOrphan("service") {
					t.Fatal("expiry left surviving child")
				}
			}
			wantStops := 0
			if tc.alive && !tc.pinned {
				wantStops = 1
			}
			if stops != wantStops {
				t.Fatalf("stops=%d want=%d", stops, wantStops)
			}
		})
	}
}

func TestLeasesRenewLimitsAndHolderIsolation(t *testing.T) {
	e, _, c := ownedEngine(t, nil, savedCommand())
	a, s := session(t, e, "a")
	b, u := session(t, e, "b")
	for _, ttl := range []string{"-1m", "0s", "8h1s", "bad"} {
		if _, err := e.OpenLease(a, s, api.OpenLeaseParams{ID: "service", TTL: ttl}); err == nil {
			t.Fatalf("accepted %q", ttl)
		}
	}
	l := openTestLease(t, e, a, s)
	readyOwned(t, e, "service")
	if err := e.CloseLease(b, u, l.ID); err == nil {
		t.Fatal("released another holder's lease")
	}
	if _, err := e.RenewLease(b, u, api.RenewLeaseParams{ID: l.ID}); err == nil {
		t.Fatal("renewed another holder's lease")
	}
	c.Advance(29 * time.Minute)
	renewed, err := e.RenewLease(a, s, api.RenewLeaseParams{ID: l.ID, TTL: "8h"})
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.Equal(c.Now().Add(MaxLeaseTTL)) {
		t.Fatalf("renewed %+v", renewed)
	}
	if _, err := e.ConnectSession(b, s); err == nil {
		t.Fatal("another actor reused session")
	}
}

func TestEphemeralSaveKeepsProcessAndPromoteIsNotStale(t *testing.T) {
	e, r, _ := ownedEngine(t, nil, model.Config{})
	a, s := session(t, e, "agent")
	result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: commandTunnel()})
	if err != nil {
		t.Fatal(err)
	}
	readyOwned(t, e, result.ID)
	receive(t, r.started)
	before := e.Overview().Tunnels[0]
	if _, err := e.SaveTunnel(commandTunnel(), false); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, result.ID, true)
	cfg, err := config.Load(e.configPath)
	if err != nil || len(cfg.Tunnels) != 1 {
		t.Fatalf("saved=%+v %v", cfg, err)
	}
	if strings.HasPrefix(cfg.Tunnels[0].ID, "tmp-") {
		t.Fatal("ephemeral leaked into TOML")
	}
	id, err := e.PromoteTunnel(api.PromoteTunnelParams{ID: result.ID, SavedID: "promoted"})
	if err != nil {
		t.Fatal(err)
	}
	var promoted api.TunnelView
	for _, v := range e.Overview().Tunnels {
		if v.Tunnel.ID == id {
			promoted = v
		}
	}
	if promoted.Status.Stale || promoted.Status.PID != before.Status.PID || promoted.Status.State != supervisor.StateRunning || promoted.Owner.Ephemeral || promoted.Owner.Leases != 1 {
		t.Fatalf("promoted=%+v before=%+v", promoted, before)
	}
	select {
	case <-r.started:
		t.Fatal("promotion restarted process")
	default:
	}
	cfg, err = config.Load(e.configPath)
	if err != nil || len(cfg.Tunnels) != 2 {
		t.Fatalf("promoted saved=%+v %v", cfg, err)
	}
	st, err := e.runtimeStore.Load()
	if err != nil || len(st.Ephemeral) != 0 || st.Leases[0].TunnelID != id {
		t.Fatalf("runtime=%+v %v", st, err)
	}
	recs, err := e.records.List()
	if err != nil || len(recs) != 1 || recs[0].TunnelID != id {
		t.Fatalf("records=%+v %v", recs, err)
	}
	if err := e.CloseLease(a, s, result.Lease.ID); err != nil {
		t.Fatal(err)
	}
	requireRunning(t, e, id, false)
}

func TestEphemeralRemovedOnStopAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			e, r, _ := ownedEngine(t, nil, model.Config{})
			a, s := session(t, e, "agent")
			result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: commandTunnel()})
			if err != nil {
				t.Fatal(err)
			}
			readyOwned(t, e, result.ID)
			p := receive(t, r.processes)
			if failure {
				p.once.Do(func() { p.exit <- errors.New("unexpected exit") })
				deadline := time.After(5 * time.Second)
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for e.known(result.ID) {
					select {
					case <-deadline:
						t.Fatal("failed ephemeral definition retained")
					case <-ticker.C:
					}
				}
			} else {
				if err := e.CloseLease(a, s, result.Lease.ID); err != nil {
					t.Fatal(err)
				}
			}
			if e.known(result.ID) {
				t.Fatal("stopped ephemeral retained")
			}
			st, err := e.runtimeStore.Load()
			if err != nil || len(st.Ephemeral) != 0 {
				t.Fatalf("runtime=%+v %v", st, err)
			}
		})
	}
}

type inspectRunner struct {
	inner   supervisor.Runner
	inspect func(supervisor.SpawnRequest) error
}

func (r inspectRunner) Start(p supervisor.SpawnRequest) (supervisor.Process, error) {
	if err := r.inspect(p); err != nil {
		return nil, err
	}
	return r.inner.Start(p)
}
func TestEphemeralPersistsDefinitionAndLeaseBeforeSpawn(t *testing.T) {
	checked := make(chan error, 1)
	e, r, _ := ownedEngine(t, func(opts *Options) {
		path := filepath.Join(opts.StateDir, "runtime.json")
		opts.Runner = inspectRunner{opts.Runner, func(supervisor.SpawnRequest) error {
			st, err := (corestate.RuntimeStore{Path: path}).Load()
			if err == nil && (len(st.Ephemeral) != 1 || len(st.Leases) != 1 || st.Leases[0].TunnelID != st.Ephemeral[0].ID) {
				err = fmt.Errorf("not persisted before spawn: %+v", st)
			}
			checked <- err
			return err
		}}
	}, model.Config{})
	a, s := session(t, e, "agent")
	result, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: commandTunnel()})
	if err != nil {
		t.Fatal(err)
	}
	if err := receive(t, checked); err != nil {
		t.Fatal(err)
	}
	receive(t, r.started)
	readyOwned(t, e, result.ID)
}

func TestEphemeralPersistenceFailureNeverSpawns(t *testing.T) {
	e, r, _ := ownedEngine(t, nil, model.Config{})
	a, s := session(t, e, "agent")
	// A directory in place of runtime.json makes atomic rename fail reliably.
	if err := os.Remove(e.runtimeStore.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.runtimeStore.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: commandTunnel()}); err == nil {
		t.Fatal("persistence failure ignored")
	}
	select {
	case <-r.started:
		t.Fatal("spawned without durable ownership")
	default:
	}
	if len(e.manager.Config().Tunnels) != 0 {
		t.Fatal("failed write published definition")
	}
}

func autoTunnel() model.Tunnel {
	return model.Tunnel{Name: "Auto SSH", Type: model.TypeSSH, SSH: &model.SSH{Host: "test", Flags: []string{"-N"}}, Forwards: []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:80"}}}
}
func TestAutoPortSkipsListenersSavedEphemeralAndOwnForwards(t *testing.T) {
	var probed []string
	cfg := model.Config{Tunnels: []model.Tunnel{{ID: "reserved", Name: "Reserved", Type: model.TypeSSH, SSH: &model.SSH{Host: "test"}, Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "22001", Target: "80"}}}}}
	e, _, _ := ownedEngine(t, func(opts *Options) {
		opts.PortRange = "22000-22006"
		opts.Listening = func(address string) bool { probed = append(probed, address); return address == "127.0.0.1:22000" }
	}, cfg)
	// The fake runner does not create SSH listeners. Keep them starting while
	// inspecting allocated definitions rather than waiting for readiness.
	a, s := session(t, e, "agent")
	first, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: autoTunnel()})
	if err != nil {
		t.Fatal(err)
	}
	secondTunnel := autoTunnel()
	secondTunnel.Forwards = append(secondTunnel.Forwards, model.Forward{Kind: model.ForwardDynamic, ListenPort: "auto"})
	second, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: secondTunnel})
	if err != nil {
		t.Fatal(err)
	}
	binds := map[string][]string{}
	for _, tunnel := range e.manager.Config().Tunnels {
		for _, f := range tunnel.Forwards {
			binds[tunnel.ID] = append(binds[tunnel.ID], f.Bind)
			if f.ListenPort != "" {
				t.Fatal("unresolved port")
			}
		}
	}
	if binds[first.ID][0] != "127.0.0.1:22002" || strings.Join(binds[second.ID], ",") != "127.0.0.1:22003,127.0.0.1:22004" {
		t.Fatalf("binds=%+v", binds)
	}
	for _, address := range probed {
		if !strings.HasPrefix(address, "127.0.0.1:") {
			t.Fatalf("probed nonloopback %s", address)
		}
	}
}
func TestAutoPortRealListeningAndExhaustion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, p, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(p)
	e, r, _ := ownedEngine(t, func(opts *Options) { opts.PortRange = fmt.Sprintf("%d-%d", port, port) }, model.Config{})
	a, s := session(t, e, "agent")
	if _, err := e.RunEphemeral(a, s, api.RunEphemeralParams{Tunnel: autoTunnel()}); err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Fatalf("allocation=%v", err)
	}
	select {
	case <-r.started:
		t.Fatal("spawned with occupied port")
	default:
	}
}

func TestOwnershipJSONShape(t *testing.T) {
	e, _, _ := ownedEngine(t, nil, savedCommand())
	if err := e.Start("service"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(e.Overview().Tunnels[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"owner":{"pinned":true,"ephemeral":false,"leases":0}`) {
		t.Fatalf("owner contract: %s", data)
	}
	if !e.Statuses()[0].Owner.Pinned {
		t.Fatal("daemon status omitted owner")
	}
}
