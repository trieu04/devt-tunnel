package control

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func TestPolicySettingsPortRangeStableAutoApprovalAndNoAgentExpiry(t *testing.T) {
	a := settings.StrictAgent()
	a.PortRange = "31234-31236"
	a.AllowTunnels = []string{"t"}
	_, socket, _ := policyServer(t, a, func(o *engine.Options, _ *model.Config) { o.Listening = func(string) bool { return false } })
	mcp := dialClient(t, socket, "mcp", "settings-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
	var opened string
	policyOK(t, mcp, "tunnel.open", api.OpenSavedParams{ID: "t"}, &opened)
	if opened != "t" || policyTunnel(t, person, "t").Owner.Holder == nil {
		t.Fatalf("agent open must hold the tunnel: %q", opened)
	}
	policyOK(t, mcp, "tunnel.release", api.IDParams{ID: "t"}, nil)
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
	if result.ID != before.TunnelID || after.Tunnel.Forwards[0].Bind != "127.0.0.1:31234" {
		t.Fatalf("auto approval drifted: before=%+v after=%+v result=%+v", before, after, result)
	}
	policyOK(t, mcp, "tunnel.release", api.IDParams{ID: result.ID}, nil)
}

func TestPolicyAlwaysSavedLiteralAndFingerprintFence(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.StrictAgent(), func(o *engine.Options, cfg *model.Config) {
		path = o.SettingsPath
		cfg.Tunnels = append(cfg.Tunnels, policySSH("literal*"), policySSH("literal-other"))
	})
	mcp := dialClient(t, socket, "mcp", "always-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
	p := api.OpenSavedParams{ID: "literal*"}
	id := policyPending(t, mcp, "tunnel.open", p)
	policyDecide(t, person, id, "approve", "always")
	cfg, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cfg.AgentPolicy().AllowTunnels, `literal\*`) {
		t.Fatalf("always persisted nonliteral grant: %+v", cfg.AgentPolicy())
	}
	other := dialClient(t, socket, "mcp", "another-agent", "")
	policyPending(t, other, "tunnel.open", api.OpenSavedParams{ID: "literal-other"})
	edited := policyTunnel(t, person, "literal*").Tunnel
	edited.SSH.Hostname = "changed-after-approval.example.invalid"
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: edited}, nil)
	next := policyPending(t, mcp, "tunnel.open", p)
	if next == id {
		t.Fatal("always approval admitted a changed first retry")
	}
	policyDecide(t, person, next, "approve", "once")
	var opened string
	policyOK(t, mcp, "tunnel.open", p, &opened)
	policyOK(t, mcp, "tunnel.release", api.IDParams{ID: opened}, nil)
	policyOK(t, other, "tunnel.open", p, &opened)
	policyOK(t, other, "tunnel.release", api.IDParams{ID: opened}, nil)
}

func TestPolicyAlwaysAliasAndPurposeBound(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.StrictAgent(), func(o *engine.Options, cfg *model.Config) {
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
	mcp := dialClient(t, socket, "mcp", "alias-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
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
	policyOK(t, mcp, "tunnel.release", api.IDParams{ID: result.ID}, nil)
}

func TestApprovalOnceConcurrentRetryCannotDoubleSpend(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	first := dialClient(t, socket, "mcp", "adapter", "")
	second := dialClient(t, socket, "mcp", "adapter", first.Session())
	person := dialClient(t, socket, "gui", "person", "")
	p := api.OpenSavedParams{ID: "t"}
	id := policyPending(t, first, "tunnel.open", p)
	policyDecide(t, person, id, "approve", "once")
	results := make(chan error, 2)
	go func() { results <- policyCall(t, first, "tunnel.open", p, nil) }()
	go func() { results <- policyCall(t, second, "tunnel.open", p, nil) }()
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
	if view := policyTunnel(t, person, "t"); view.Owner.Holder == nil {
		t.Fatalf("once double spend lost the hold: %+v", view.Owner)
	}
	policyOK(t, first, "tunnel.release", api.IDParams{ID: "t"}, nil)
}

func TestAgentSettingsPersonOnlyAndValidated(t *testing.T) {
	var path string
	_, socket, _ := policyServer(t, settings.StrictAgent(), func(o *engine.Options, _ *model.Config) { path = o.SettingsPath })
	person := dialClient(t, socket, "gui", "person", "")
	mcp := dialClient(t, socket, "mcp", "settings-agent", "")
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
	policyOK(t, person, "settings.setAgent", api.AgentSettingsParams{Agent: next}, nil)
	var got settings.Agent
	policyOK(t, person, "settings.getAgent", api.Empty{}, &got)
	if got.Enabled || got.Public != "deny" || got.PortRange != next.PortRange || !slices.Equal(got.AllowTunnels, next.AllowTunnels) || !slices.Equal(got.AllowSSHHosts, next.AllowSSHHosts) {
		t.Fatalf("policy did not round trip: %+v", got)
	}
	saved, err := settings.Load(path)
	if err != nil || saved.AgentPolicy().Public != "deny" || saved.AgentPolicy().Enabled {
		t.Fatalf("policy did not persist: %+v / %v", saved, err)
	}
	invalid := next
	invalid.Public = "bogus"
	if err := policyCall(t, person, "settings.setAgent", api.AgentSettingsParams{Agent: invalid}, nil); err == nil {
		t.Fatal("invalid public policy accepted")
	}
	// Both methods must stay outside mcpMethodAllowed, even read access.
	for _, method := range []string{"settings.getAgent", "settings.setAgent"} {
		policyError(t, policyCall(t, mcp, method, api.AgentSettingsParams{Agent: initial}, nil), api.AgentDisabled)
		if mcpMethodAllowed(method) {
			t.Fatalf("settings method entered MCP allow-list: %s", method)
		}
	}
	policyOK(t, person, "settings.getAgent", api.Empty{}, &got)
	if got.Public != "deny" || got.Enabled {
		t.Fatalf("refused changes mutated policy: %+v", got)
	}
}
