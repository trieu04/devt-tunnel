package control

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func TestPolicySettingsTTLsPortRangeAndStableAutoApproval(t *testing.T) {
	a := settings.DefaultAgent()
	a.DefaultTTL = "75s"
	a.MaxTTL = "2m"
	a.PortRange = "31234-31236"
	a.AllowTunnels = []string{"t"}
	_, socket, clock := policyServer(t, a, func(o *engine.Options, _ *model.Config) { o.Listening = func(string) bool { return false } })
	mcp := leasedDial(t, socket, "mcp", "settings-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	var lease api.Lease
	policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, &lease)
	if want := clock.Now().Add(75 * time.Second); !lease.ExpiresAt.Equal(want) {
		t.Fatalf("default expiry=%s want=%s", lease.ExpiresAt, want)
	}
	policyOK(t, mcp, "lease.renew", api.RenewLeaseParams{ID: lease.ID, TTL: "2m"}, &lease)
	if !lease.ExpiresAt.Equal(clock.Now().Add(2 * time.Minute)) {
		t.Fatal("max TTL was not applied")
	}
	if err := policyCall(t, mcp, "lease.renew", api.RenewLeaseParams{ID: lease.ID, TTL: "2m1ns"}, nil); err == nil {
		t.Fatal("accepted TTL above configured max")
	}
	policyOK(t, mcp, "lease.close", api.IDParams{ID: lease.ID}, nil)
	tunnel := policySSH("")
	tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:5432"}}
	p := api.RunEphemeralParams{Tunnel: tunnel}
	id := policyPending(t, mcp, "tunnel.runEphemeral", p)
	before := policyGetApproval(t, person, id)
	if !strings.Contains(strings.Join(before.Argv, " "), "127.0.0.1:31234:") {
		t.Fatalf("configured range not used: %v", before.Argv)
	}
	policyDecide(t, person, id, "approve", "once")
	var result api.RunEphemeralResult
	policyOK(t, mcp, "tunnel.runEphemeral", p, &result)
	after := policyTunnel(t, person, result.ID)
	if result.ID != before.TunnelID || after.Tunnel.Forwards[0].Bind != "127.0.0.1:31234" || !result.Lease.ExpiresAt.Equal(clock.Now().Add(75*time.Second)) {
		t.Fatalf("auto approval drifted: before=%+v after=%+v result=%+v", before, after, result)
	}
	policyOK(t, mcp, "lease.close", api.IDParams{ID: result.Lease.ID}, nil)
}

func TestPolicyAlwaysSavedLiteralAndFingerprintFence(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, cfg *model.Config) {
		path = o.SettingsPath
		cfg.Tunnels = append(cfg.Tunnels, policySSH("literal*"), policySSH("literal-other"))
	})
	mcp := leasedDial(t, socket, "mcp", "always-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	p := api.OpenLeaseParams{ID: "literal*"}
	id := policyPending(t, mcp, "lease.open", p)
	policyDecide(t, person, id, "approve", "always")
	cfg, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.AgentPolicy().AllowTunnels, `literal\*`) {
		t.Fatalf("always persisted nonliteral grant: %+v", cfg.AgentPolicy())
	}
	other := leasedDial(t, socket, "mcp", "another-agent", "")
	policyPending(t, other, "lease.open", api.OpenLeaseParams{ID: "literal-other"})
	edited := policyTunnel(t, person, "literal*").Tunnel
	edited.SSH.Hostname = "changed-after-approval.example.invalid"
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: edited}, nil)
	next := policyPending(t, mcp, "lease.open", p)
	if next == id {
		t.Fatal("always approval admitted a changed first retry")
	}
	policyDecide(t, person, next, "approve", "once")
	var lease api.Lease
	policyOK(t, mcp, "lease.open", p, &lease)
	policyOK(t, mcp, "lease.close", api.IDParams{ID: lease.ID}, nil)
	policyOK(t, other, "lease.open", p, &lease)
	policyOK(t, other, "lease.close", api.IDParams{ID: lease.ID}, nil)
}

func TestPolicyAlwaysAliasAndPurposeBound(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, cfg *model.Config) {
		path = o.SettingsPath
		sshPath := filepath.Join(filepath.Dir(o.ConfigPath), ".ssh", "config")
		if err := os.MkdirAll(filepath.Dir(sshPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(sshPath, []byte("Host bastion\n HostName example.invalid\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_ = cfg
	})
	mcp := leasedDial(t, socket, "mcp", "alias-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := model.Tunnel{Name: "Alias", Type: model.TypeSSH, SSH: &model.SSH{Host: "bastion", Flags: []string{"-N"}}, Forwards: []model.Forward{{Kind: model.ForwardDynamic, Bind: "127.0.0.1:31237"}}}
	p := api.RunEphemeralParams{Tunnel: tunnel, Purpose: strings.Repeat("x", engine.MaxApprovalPurpose+1)}
	if err := policyCall(t, mcp, "tunnel.runEphemeral", p, nil); err == nil {
		t.Fatal("unbounded purpose accepted")
	}
	var pending []api.Approval
	policyOK(t, person, "approval.list", api.Empty{}, &pending)
	if len(pending) != 0 {
		t.Fatal("invalid purpose queued approval")
	}
	p.Purpose = "A short purpose"
	id := policyPending(t, mcp, "tunnel.runEphemeral", p)
	policyDecide(t, person, id, "approve", "always")
	cfg, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.AgentPolicy().AllowSSHHosts, "bastion") {
		t.Fatal("alias grant not persisted")
	}
	var result api.RunEphemeralResult
	policyOK(t, mcp, "tunnel.runEphemeral", p, &result)
	policyOK(t, mcp, "lease.close", api.IDParams{ID: result.Lease.ID}, nil)
}

func TestApprovalOnceConcurrentRetryCannotDoubleSpend(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	first := leasedDial(t, socket, "mcp", "adapter", "")
	second := leasedDial(t, socket, "mcp", "adapter", first.Session())
	person := leasedDial(t, socket, "gui", "person", "")
	p := api.OpenLeaseParams{ID: "t"}
	id := policyPending(t, first, "lease.open", p)
	policyDecide(t, person, id, "approve", "once")
	results := make(chan error, 2)
	go func() { results <- policyCall(t, first, "lease.open", p, nil) }()
	go func() { results <- policyCall(t, second, "lease.open", p, nil) }()
	success, pending := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			policyError(t, err, api.PendingApproval)
			pending++
		}
	}
	if success != 1 || pending != 1 {
		t.Fatalf("once double spend: success=%d pending=%d", success, pending)
	}
	var leases []api.Lease
	policyOK(t, first, "lease.list", api.Empty{}, &leases)
	if len(leases) != 1 {
		t.Fatalf("once double spend leases=%+v", leases)
	}
	policyOK(t, first, "lease.close", api.IDParams{ID: leases[0].ID}, nil)
}

func TestAgentSettingsPersonOnlyAndValidated(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, _ *model.Config) { path = o.SettingsPath })
	person := leasedDial(t, socket, "gui", "person", "")
	mcp := leasedDial(t, socket, "mcp", "settings-agent", "")
	var initial settings.Agent
	policyOK(t, person, "settings.getAgent", api.Empty{}, &initial)
	if initial.Public != "ask" || !initial.Enabled || len(initial.AllowTunnels) != 0 {
		t.Fatalf("unexpected default policy: %+v", initial)
	}
	next := initial
	next.Enabled = false
	next.Public = "deny"
	next.AllowTunnels = []string{"dev-*"}
	next.AllowSSHHosts = []string{"staging"}
	next.PortRange = "31000-31010"
	next.DefaultTTL, next.MaxTTL = "15m", "1h"
	policyOK(t, person, "settings.setAgent", api.AgentSettingsParams{Agent: next}, nil)
	var got settings.Agent
	policyOK(t, person, "settings.getAgent", api.Empty{}, &got)
	if got.Enabled || got.Public != "deny" || got.PortRange != next.PortRange || !slices.Equal(got.AllowTunnels, next.AllowTunnels) || !slices.Equal(got.AllowSSHHosts, next.AllowSSHHosts) || got.DefaultTTL != "15m" || got.MaxTTL != "1h" {
		t.Fatalf("policy did not round trip: %+v", got)
	}
	saved, err := settings.Load(path)
	if err != nil || saved.AgentPolicy().Public != "deny" || saved.AgentPolicy().Enabled {
		t.Fatalf("policy did not persist: %+v / %v", saved, err)
	}
	invalid := next
	invalid.Public = "allow"
	if err := policyCall(t, person, "settings.setAgent", api.AgentSettingsParams{Agent: invalid}, nil); err == nil {
		t.Fatal("invalid public policy accepted")
	}
	// Both methods must stay outside mcpMethodAllowed, even read access.
	for _, method := range []string{"settings.getAgent", "settings.setAgent"} {
		policyError(t, policyCall(t, mcp, method, api.AgentSettingsParams{Agent: initial}, nil), api.PolicyDenied)
	}
	policyOK(t, person, "settings.getAgent", api.Empty{}, &got)
	if got.Public != "deny" || got.Enabled {
		t.Fatalf("refused changes mutated policy: %+v", got)
	}
}
