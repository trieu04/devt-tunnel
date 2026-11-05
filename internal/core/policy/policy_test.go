package policy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
)

var agent = Actor{Kind: ActorMCP, Name: "test-agent"}

func localTunnel() model.Tunnel {
	return model.Tunnel{ID: "dev-db", Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Flags: []string{"-N"}, Options: map[string]string{"ServerAliveInterval": "30"}}, Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:20000", Target: "db.internal:5432"}}}
}

func configWithGrants() settings.Settings {
	a := settings.DefaultAgent()
	a.AllowTunnels, a.AllowSSHHosts = []string{"dev-*"}, []string{"staging"}
	return settings.Settings{Agent: &a}
}

func wantDecision(t *testing.T, action Action, tunnel model.Tunnel, cfg settings.Settings, want Decision) {
	t.Helper()
	got, reason := Decide(agent, action, tunnel, cfg)
	if got != want || reason == "" {
		t.Fatalf("Decide(%s)=%s,%q; want %s and a reason", action, got, reason, want)
	}
}

// Every numbered group corresponds to a row in control-plane.md's policy
// table. Other tests below cover precedence, saved-vs-new and approval scope.
func TestPolicyTable(t *testing.T) {
	t.Run("row_1_read_only", func(t *testing.T) {
		// Listing/checking must not execute a saved command or require approval.
		unsafe := model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: "echo unsafe"}}
		for _, action := range []Action{ActionListTunnels, ActionListSSHHosts, ActionReadLogs, ActionCheck} {
			t.Run(string(action), func(t *testing.T) { wantDecision(t, action, unsafe, settings.Settings{}, Allow) })
		}
	})
	t.Run("row_2_open_saved", func(t *testing.T) {
		for _, action := range []Action{ActionOpenSaved, ActionStartSaved} {
			t.Run(string(action), func(t *testing.T) {
				wantDecision(t, action, localTunnel(), settings.Settings{}, Ask)
				wantDecision(t, action, localTunnel(), configWithGrants(), Allow)
				tunnel := localTunnel()
				tunnel.ID = "prod-db"
				wantDecision(t, action, tunnel, configWithGrants(), Ask)
				cfg := configWithGrants()
				cfg.Agent.AllowTunnels = nil // A host grant is not a saved-tunnel grant.
				wantDecision(t, action, localTunnel(), cfg, Ask)
			})
		}
	})
	t.Run("row_3_new_local_alias", func(t *testing.T) {
		for _, kind := range []model.ForwardKind{model.ForwardLocal, model.ForwardDynamic} {
			t.Run(string(kind), func(t *testing.T) {
				tunnel := localTunnel()
				tunnel.Forwards[0].Kind = kind
				for _, action := range []Action{ActionOpen, ActionCreate} {
					wantDecision(t, action, tunnel, settings.Settings{}, Ask)
					wantDecision(t, action, tunnel, configWithGrants(), Allow)
				}
			})
		}
		tunnel := localTunnel()
		tunnel.SSH.Host = "production"
		wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
		cfg := configWithGrants()
		cfg.Agent.AllowSSHHosts = []string{"stag*"} // Host grants are literal aliases, not globs.
		wantDecision(t, ActionOpen, localTunnel(), cfg, Ask)
	})
	t.Run("row_4_manual_hostname", func(t *testing.T) {
		tunnel := localTunnel()
		tunnel.SSH.Host, tunnel.SSH.Hostname = "", "staging"
		wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
		if CanAlwaysAllow(agent, ActionOpen, tunnel, configWithGrants()) {
			t.Fatal("manual hostname offered persistent approval")
		}
	})
	t.Run("row_5_public_exposure", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			tunnel model.Tunnel
		}{
			{"ngrok", model.Tunnel{ID: "dev-public", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "127.0.0.1:3000"}}},
			{"cloudflared", model.Tunnel{ID: "dev-public", Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://127.0.0.1:3000"}}},
			{"remote_loopback", withForward(model.ForwardRemote, "127.0.0.1:20000")},
			{"remote_unix", withForward(model.ForwardRemote, "/tmp/remote.sock")},
			{"wildcard", withForward(model.ForwardLocal, "0.0.0.0:20000")},
			{"LAN", withForward(model.ForwardDynamic, "192.168.1.2:20000")},
			{"IPv6_wildcard", withForward(model.ForwardLocal, "[::]:20000")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for _, action := range []Action{ActionOpen, ActionExpose, ActionOpenSaved, ActionStartSaved} {
					cfg := configWithGrants()
					wantDecision(t, action, tc.tunnel, cfg, Ask)
					if CanAlwaysAllow(agent, action, tc.tunnel, cfg) {
						t.Fatal("public exposure offered persistent approval")
					}
					cfg.Agent.Public = "deny"
					wantDecision(t, action, tc.tunnel, cfg, Deny)
				}
			})
		}
		// An expose action cannot be made always-able by disguising the bind.
		wantDecision(t, ActionExpose, localTunnel(), configWithGrants(), Ask)
		if CanAlwaysAllow(agent, ActionExpose, localTunnel(), configWithGrants()) {
			t.Fatal("expose offered persistent approval")
		}
	})
	t.Run("row_6_save", func(t *testing.T) {
		wantDecision(t, ActionSave, localTunnel(), configWithGrants(), Ask)
		if CanAlwaysAllow(agent, ActionSave, localTunnel(), configWithGrants()) {
			t.Fatal("save offered persistent approval")
		}
	})
	t.Run("row_7_existing_secret_new_tunnel", func(t *testing.T) {
		tunnel := localTunnel()
		tunnel.SSH.PassphraseSecret = "ssh-key-passphrase"
		wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
		wantDecision(t, ActionUseSecret, tunnel, configWithGrants(), Ask)
		if CanAlwaysAllow(agent, ActionOpen, tunnel, configWithGrants()) {
			t.Fatal("new secret reference offered persistent approval")
		}
		// Saved definitions already name their own secrets; only NEW use asks.
		wantDecision(t, ActionOpenSaved, tunnel, configWithGrants(), Allow)
	})
	t.Run("row_8_execution_extensions", func(t *testing.T) {
		for _, action := range []Action{ActionCreateCommand, ActionEditCommand, ActionSetFlags, ActionSetOptions, ActionSetExtraArgs, ActionSetIdentity} {
			t.Run(string(action), func(t *testing.T) { wantDecision(t, action, localTunnel(), configWithGrants(), Deny) })
		}
		for _, tc := range unsafeTunnels() {
			t.Run(tc.name, func(t *testing.T) {
				for _, action := range []Action{ActionOpen, ActionCreate, ActionSave} {
					wantDecision(t, action, tc.tunnel, configWithGrants(), Deny)
				}
			})
		}
	})
	t.Run("row_9_person_only", func(t *testing.T) {
		for _, action := range []Action{ActionReadSecret, ActionSetSecret, ActionRenameSecret, ActionDeleteSecret, ActionAnswerPrompt, ActionEditPolicy, ActionEditSaved, ActionDeleteSaved, ActionStop} {
			t.Run(string(action), func(t *testing.T) { wantDecision(t, action, localTunnel(), configWithGrants(), Deny) })
		}
	})
}

func withForward(kind model.ForwardKind, bind string) model.Tunnel {
	tunnel := localTunnel()
	tunnel.Forwards[0].Kind, tunnel.Forwards[0].Bind = kind, bind
	return tunnel
}

func unsafeTunnels() []struct {
	name   string
	tunnel model.Tunnel
} {
	cases := []struct {
		name   string
		tunnel model.Tunnel
	}{{"command", model.Tunnel{ID: "dev-command", Type: model.TypeCommand, Command: &model.Command{Line: "echo unsafe"}}}}
	for _, option := range []string{"ProxyCommand", "localcommand", "PERMITLOCALCOMMAND", "ProxyJump"} {
		tunnel := localTunnel()
		tunnel.SSH.Options[option] = "unsafe"
		cases = append(cases, struct {
			name   string
			tunnel model.Tunnel
		}{option, tunnel})
	}
	for _, extension := range []string{"flags", "extra_args", "identity"} {
		tunnel := localTunnel()
		switch extension {
		case "flags":
			tunnel.SSH.Flags = []string{"-N", "-F/tmp/config"}
		case "extra_args":
			tunnel.SSH.ExtraArgs = "-oProxyCommand=unsafe"
		case "identity":
			tunnel.SSH.Identity = "/tmp/key"
		}
		cases = append(cases, struct {
			name   string
			tunnel model.Tunnel
		}{extension, tunnel})
	}
	return cases
}

func TestUnsafeSavedTunnelAlwaysAsksWithoutPersistentGrant(t *testing.T) {
	for _, tc := range unsafeTunnels() {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []Action{ActionOpenSaved, ActionStartSaved} {
				for _, cfg := range []settings.Settings{{}, configWithGrants()} {
					wantDecision(t, action, tc.tunnel, cfg, Ask)
					if CanAlwaysAllow(agent, action, tc.tunnel, cfg) {
						t.Fatal("unsafe saved tunnel offered persistent approval")
					}
				}
			}
		})
	}
	// Public deny remains stronger than the approval path for unsafe saved specs.
	tunnel := localTunnel()
	tunnel.SSH.Options["ProxyCommand"] = "unsafe"
	tunnel.Forwards[0].Kind = model.ForwardRemote
	cfg := configWithGrants()
	cfg.Agent.Public = "deny"
	wantDecision(t, ActionOpenSaved, tunnel, cfg, Deny)
}

func TestAlwaysApprovalEligibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action Action
		tunnel model.Tunnel
		want   bool
	}{
		{"safe_saved", ActionOpenSaved, localTunnel(), true},
		{"safe_start_saved", ActionStartSaved, localTunnel(), true},
		{"local_alias", ActionOpen, localTunnel(), true},
		{"create_local_alias", ActionCreate, localTunnel(), true},
		{"save", ActionSave, localTunnel(), false},
		{"use_secret", ActionUseSecret, localTunnel(), false},
		{"bare_port", ActionOpen, withForward(model.ForwardLocal, "20000"), false},
		{"IPv6_loopback", ActionOpen, withForward(model.ForwardLocal, "[::1]:20000"), false},
		{"localhost", ActionOpen, withForward(model.ForwardLocal, "localhost:20000"), false},
		{"Unix_socket", ActionOpen, withForward(model.ForwardLocal, "/tmp/forward.sock"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanAlwaysAllow(agent, tc.action, tc.tunnel, settings.Settings{}); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if CanAlwaysAllow(agent, ActionOpenSaved, localTunnel(), configWithGrants()) {
		t.Fatal("no approval choices should be offered for an already allowed action")
	}
}

func TestActorAndDisabledPolicy(t *testing.T) {
	cfg := configWithGrants()
	cfg.Agent.Enabled = false
	for _, action := range []Action{ActionListTunnels, ActionOpen, ActionOpenSaved, ActionCloseLease} {
		wantDecision(t, action, localTunnel(), cfg, Deny)
	}
	for _, kind := range []ActorKind{ActorCLI, ActorGUI} {
		for _, action := range []Action{ActionOpen, ActionOpenSaved, ActionStop, ActionCreateCommand, ActionReadSecret, ActionEditPolicy} {
			got, reason := Decide(Actor{Kind: kind, Name: "test-person"}, action, unsafeTunnels()[0].tunnel, cfg)
			if got != Allow || reason == "" {
				t.Fatalf("%s %s=%s %q", kind, action, got, reason)
			}
		}
	}
	for _, kind := range []ActorKind{"", "MCP", "unknown"} {
		if got, _ := Decide(Actor{Kind: kind}, ActionListTunnels, localTunnel(), settings.Settings{}); got != Deny {
			t.Fatalf("unknown actor %q allowed", kind)
		}
	}
	for _, kind := range []ActorKind{ActorMCP, ActorCLI, ActorGUI} {
		if got, _ := Decide(Actor{Kind: kind}, "new_unknown_action", localTunnel(), settings.Settings{}); got != Deny {
			t.Fatalf("unknown action allowed for %s", kind)
		}
	}
	// Actor name does not confer any privilege or bypass MCP rules.
	for _, name := range []string{"gui", "cli", "root", ""} {
		if got, _ := Decide(Actor{Kind: ActorMCP, Name: name}, ActionStop, localTunnel(), configWithGrants()); got != Deny {
			t.Fatalf("name %q conferred privilege", name)
		}
	}
}

func TestLeaseActions(t *testing.T) {
	for _, action := range []Action{ActionListLeases, ActionCloseLease, ActionRenewLease} {
		wantDecision(t, action, model.Tunnel{}, settings.Settings{}, Allow)
	}
	wantDecision(t, ActionStop, localTunnel(), configWithGrants(), Deny)
}

func TestPublicExposureBinds(t *testing.T) {
	for _, tc := range []struct {
		bind string
		want bool
	}{
		{"127.0.0.1:20000", false}, {"127.3.2.1:20000", false}, {"[::1]:20000", false},
		{"[::1%lo0]:20000", false}, {"[::ffff:127.0.0.1]:20000", false}, {"LOCALHOST:20000", false},
		{"20000", false}, {"/tmp/forward.sock", false},
		{"0.0.0.0:20000", true}, {"[::]:20000", true}, {"*:20000", true}, {":20000", true},
		{"192.168.1.2:20000", true}, {"example.com:20000", true}, {"[2001:db8::1]:20000", true},
		{"", true}, {"127.0.0.1:bad", true}, {"127.0.0.1:65536", true}, {"not-a-bind", true},
	} {
		t.Run(tc.bind, func(t *testing.T) {
			tunnel := withForward(model.ForwardLocal, tc.bind)
			if got := PublicExposure(tunnel); got != tc.want {
				t.Fatalf("PublicExposure(%q)=%v want %v", tc.bind, got, tc.want)
			}
			if agentError(tunnel) != "" {
				wantDecision(t, ActionOpen, tunnel, configWithGrants(), Deny)
			} else if tc.want {
				wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
			} else if tc.bind != "127.0.0.1:20000" {
				// Alias grants only cover the explicit 127.0.0.1 L/D schema.
				wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
			}
		})
	}
}

func TestAllForwardsMustBeSafeForAliasGrant(t *testing.T) {
	tunnel := localTunnel()
	tunnel.Forwards = append(tunnel.Forwards, model.Forward{Kind: model.ForwardDynamic, Bind: "127.0.0.1:20001"})
	wantDecision(t, ActionOpen, tunnel, configWithGrants(), Allow)
	tunnel.Forwards[1].Kind = model.ForwardRemote
	wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
	tunnel.Forwards = nil
	wantDecision(t, ActionOpen, tunnel, configWithGrants(), Ask)
}

func TestInvalidSettingsFailClosed(t *testing.T) {
	for _, alter := range []func(*settings.Agent){
		func(a *settings.Agent) { a.Public = "allow" },
		func(a *settings.Agent) { a.AllowTunnels = []string{"["} },
		func(a *settings.Agent) { a.PortRange = "0-1" },
		func(a *settings.Agent) { a.MaxTTL = "1m" },
	} {
		cfg := configWithGrants()
		alter(cfg.Agent)
		wantDecision(t, ActionOpenSaved, localTunnel(), cfg, Deny)
		if CanAlwaysAllow(agent, ActionOpenSaved, localTunnel(), cfg) {
			t.Fatal("invalid policy offered persistent approval")
		}
	}
}

func TestDecidePureAndDeterministic(t *testing.T) {
	cfg := configWithGrants()
	tunnel := unsafeTunnels()[1].tunnel
	snapshot := func() []byte {
		t.Helper()
		b, err := json.Marshal([]any{agent, cfg, tunnel})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	before := snapshot()
	want, reason := Decide(agent, ActionOpenSaved, tunnel, cfg)
	for range 50 {
		got, gotReason := Decide(agent, ActionOpenSaved, tunnel, cfg)
		if got != want || gotReason != reason {
			t.Fatalf("nondeterministic: %s %q vs %s %q", got, gotReason, want, reason)
		}
		CanAlwaysAllow(agent, ActionOpenSaved, tunnel, cfg)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("policy mutated its inputs")
	}
}

func TestPublicExposureRequiresUnambiguousUnixSocket(t *testing.T) {
	for _, tc := range []struct {
		bind   string
		public bool
	}{
		{"/tmp/local.sock", false}, {"./local.sock", false}, {"relative/local.sock", true},
		{"0.0.0.0:8081:x/y", true}, {"127.0.0.1:8081:x/y", true}, {"/tmp/local.sock:8081", true}, {"./local.sock:8081", true},
	} {
		t.Run(tc.bind, func(t *testing.T) {
			if got := PublicExposure(withForward(model.ForwardLocal, tc.bind)); got != tc.public {
				t.Fatalf("PublicExposure=%v, want %v", got, tc.public)
			}
		})
	}
}
