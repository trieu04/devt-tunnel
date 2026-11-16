package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func policySSH(id string) model.Tunnel {
	return model.Tunnel{
		ID: id, Name: "Policy SSH", Type: model.TypeSSH,
		SSH: &model.SSH{Hostname: "bastion.example.invalid", Flags: []string{"-N"}},
	}
}

// Replace testServerWithOptions' unsafe command fixture before engine.New loads
// either file. All execution uses fakeRunner, never the host's SSH client.
func policyServer(t *testing.T, agent settings.Agent, prepare func(*engine.Options, *model.Config)) (*Server, string, *testClock) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	s, socket := testServerWithOptions(t, func(opts *engine.Options) {
		opts.Clock = clock
		opts.Runner = fakeRunner{}
		cfg := model.Config{Tunnels: []model.Tunnel{policySSH("t")}}
		if prepare != nil {
			prepare(opts, &cfg)
		}
		if err := config.Save(opts.ConfigPath, cfg); err != nil {
			t.Fatal(err)
		}
		// Resolution still requires an existing executable, although the fake
		// runner never executes it. Do not depend on locally installed tools.
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := settings.Save(opts.SettingsPath, settings.Settings{
			Agent: &agent, Tools: map[string]string{"ssh": executable, "ngrok": executable, "cloudflared": executable},
		}); err != nil {
			t.Fatal(err)
		}
	})
	return s, socket, clock
}

func policyCall(t *testing.T, c *client.Client, method string, params, result any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

func policyOK(t *testing.T, c *client.Client, method string, params, result any) {
	t.Helper()
	if err := policyCall(t, c, method, params, result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func policyError(t *testing.T, err error, kind api.ErrorKind) *api.Error {
	t.Helper()
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != kind {
		t.Fatalf("error = %v, want API kind %s", err, kind)
	}
	if kind == api.PendingApproval && rpc.Data.ApprovalID == "" {
		t.Fatal("pending approval has no approval ID")
	}
	if kind == api.PolicyDenied && rpc.Data.ApprovalID != "" {
		t.Fatalf("denial unexpectedly created approval %s", rpc.Data.ApprovalID)
	}
	return rpc
}

func policyPending(t *testing.T, c *client.Client, method string, params any) string {
	t.Helper()
	return policyError(t, policyCall(t, c, method, params, nil), api.PendingApproval).Data.ApprovalID
}

func policyGetApproval(t *testing.T, c *client.Client, id string) api.Approval {
	t.Helper()
	var approval api.Approval
	policyOK(t, c, "approval.get", api.IDParams{ID: id}, &approval)
	if approval.ID != id {
		t.Fatalf("approval ID = %q, want %q", approval.ID, id)
	}
	return approval
}

func policyDecide(t *testing.T, person *client.Client, id, decision, scope string) {
	t.Helper()
	a := policyGetApproval(t, person, id)
	if a.Code == "" || len(a.Argv) == 0 {
		t.Fatalf("person cannot inspect approval: %+v", a)
	}
	var result api.Approval
	policyOK(t, person, "approval.decide", api.DecideApprovalParams{
		ID: id, Decision: decision, Scope: scope, Code: a.Code,
	}, &result)
	want := "approved"
	if decision == "deny" {
		want = "denied"
	}
	if result.Status != want || result.Scope != scope {
		t.Fatalf("decision result = %+v", result)
	}
}

func policyNoHolders(t *testing.T, person *client.Client) {
	t.Helper()
	var views []api.TunnelView
	policyOK(t, person, "tunnel.list", api.Empty{}, &views)
	for _, v := range views {
		if v.Owner.Holder != nil {
			t.Fatalf("unapproved operation acquired a hold: %+v", v)
		}
	}
}

func policyTunnel(t *testing.T, c *client.Client, id string) api.TunnelView {
	t.Helper()
	var view api.TunnelView
	policyOK(t, c, "tunnel.get", api.IDParams{ID: id}, &view)
	return view
}

func policyStopped(t *testing.T, c *client.Client, id string) {
	t.Helper()
	view := policyTunnel(t, c, id)
	if view.Status.State != supervisor.StateStopped || view.Status.PID != 0 {
		t.Fatalf("unapproved tunnel is active: %+v", view.Status)
	}
}

func TestPolicyIntegrationSavedAndUnsafeRows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		make    func() model.Tunnel
		allowed bool
		want    api.ErrorKind
		always  bool
	}{
		{"saved allow_tunnels", func() model.Tunnel { return policySSH("row") }, true, "", false},
		{"saved nonlisted", func() model.Tunnel { return policySSH("row") }, false, api.PendingApproval, true},
		{"saved allowlisted command", func() model.Tunnel {
			return model.Tunnel{ID: "row", Name: "Human command", Type: model.TypeCommand, Command: &model.Command{Line: "echo never-execute", ReadyLog: "ready"}}
		}, true, api.PendingApproval, false},
		{"saved allowlisted ProxyCommand", func() model.Tunnel {
			tunnel := policySSH("row")
			tunnel.SSH.Options = map[string]string{"ProxyCommand": "echo never-execute"}
			return tunnel
		}, true, api.PendingApproval, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := settings.StrictAgent()
			if tc.allowed {
				agent.AllowTunnels = []string{"ro*"}
			}
			_, socket, _ := policyServer(t, agent, func(_ *engine.Options, cfg *model.Config) {
				cfg.Tunnels = append(cfg.Tunnels, tc.make())
			})
			mcp := dialClient(t, socket, "mcp", "row-agent", "")
			person := dialClient(t, socket, "gui", "reviewer", "")
			params := api.OpenSavedParams{ID: "row", Purpose: "test policy row"}
			var opened string
			err := policyCall(t, mcp, "tunnel.open", params, &opened)
			if tc.want == "" {
				if err != nil || opened != "row" || policyTunnel(t, person, "row").Owner.Holder == nil {
					t.Fatalf("allowlisted saved open: opened=%q error=%v", opened, err)
				}
				var approvals []api.Approval
				policyOK(t, person, "approval.list", api.Empty{}, &approvals)
				if len(approvals) != 0 {
					t.Fatalf("allow grant queued approval: %+v", approvals)
				}
				return
			}
			id := policyError(t, err, tc.want).Data.ApprovalID
			a := policyGetApproval(t, person, id)
			if a.Status != "pending" || a.Actor != (api.Actor{Kind: "mcp", Name: "row-agent"}) || a.Session != mcp.Session() || a.Purpose != params.Purpose || a.Fingerprint == "" {
				t.Fatalf("incorrect approval binding: %+v", a)
			}
			if !slices.Contains(a.Choices, "once") || !slices.Contains(a.Choices, "session") || slices.Contains(a.Choices, "always") != tc.always {
				t.Fatalf("approval choices = %v, always=%v", a.Choices, tc.always)
			}
			if again := policyPending(t, mcp, "tunnel.open", params); again != id {
				t.Fatalf("unchanged retry created %s, want %s", again, id)
			}
			if !tc.always {
				policyError(t, policyCall(t, person, "approval.decide", api.DecideApprovalParams{ID: id, Decision: "approve", Scope: "always", Code: a.Code}, nil), api.PolicyDenied)
				if got := policyGetApproval(t, person, id).Status; got != "pending" {
					t.Fatalf("invalid persistent grant changed status to %s", got)
				}
			}
			policyNoHolders(t, person)
			policyStopped(t, person, "row")
		})
	}
}

func TestPolicyIntegrationEphemeralExecutionExtensionsDenied(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.Tunnel)
	}{
		{"ProxyCommand option", func(t *model.Tunnel) { t.SSH.Options = map[string]string{"ProxyCommand": "echo never-execute"} }},
		{"ProxyCommand extra args", func(t *model.Tunnel) { t.SSH.ExtraArgs = "-oProxyCommand=echo" }},
		{"arbitrary flag", func(t *model.Tunnel) { t.SSH.Flags = []string{"-F", "/tmp/agent-ssh-config"} }},
		{"identity path", func(t *model.Tunnel) { t.SSH.Identity = "/tmp/agent-key" }},
		{"command", func(t *model.Tunnel) {
			t.Type = model.TypeCommand
			t.Command = &model.Command{Line: "echo never-execute"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := settings.StrictAgent()
			agent.AllowTunnels = []string{"*"}
			agent.AllowSSHHosts = []string{"bastion"}
			_, socket, _ := policyServer(t, agent, nil)
			mcp := dialClient(t, socket, "mcp", "agent", "")
			person := dialClient(t, socket, "cli", "person", "")
			tunnel := policySSH("")
			tc.mutate(&tunnel)
			policyError(t, policyCall(t, mcp, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}, nil), api.PolicyDenied)
			policyError(t, policyCall(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel}, nil), api.PolicyDenied)
			var approvals []api.Approval
			policyOK(t, person, "approval.list", api.Empty{}, &approvals)
			if len(approvals) != 0 {
				t.Fatalf("unsafe ephemeral queued approval: %+v", approvals)
			}
			policyNoHolders(t, person)
		})
	}
}

func TestPolicyIntegrationPublicExposureOverridesAllowLists(t *testing.T) {
	for _, public := range []string{"ask", "deny"} {
		for _, kind := range []string{"ngrok", "remote forward", "wildcard listener"} {
			for _, saved := range []bool{true, false} {
				mode := "ephemeral"
				if saved {
					mode = "saved"
				}
				t.Run(public+"/"+kind+"/"+mode, func(t *testing.T) {
					agent := settings.StrictAgent()
					agent.Public = public
					agent.AllowTunnels = []string{"*"}
					agent.AllowSSHHosts = []string{"bastion"}
					tunnel := policySSH("public")
					switch kind {
					case "ngrok":
						tunnel = model.Tunnel{ID: "public", Name: "Public ngrok", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "http://127.0.0.1:3000"}}
					case "remote forward":
						// Even a loopback -R requires public consent.
						tunnel.Forwards = []model.Forward{{Kind: model.ForwardRemote, Bind: "127.0.0.1:24321", Target: "127.0.0.1:5432"}}
					case "wildcard listener":
						tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: "0.0.0.0:24321", Target: "127.0.0.1:5432"}}
					}
					_, socket, _ := policyServer(t, agent, func(_ *engine.Options, cfg *model.Config) {
						if saved {
							cfg.Tunnels = append(cfg.Tunnels, tunnel)
						}
					})
					mcp := dialClient(t, socket, "mcp", "public-agent", "")
					person := dialClient(t, socket, "gui", "reviewer", "")
					method, params := "tunnel.open", any(api.OpenSavedParams{ID: tunnel.ID})
					if !saved {
						tunnel.ID = ""
						method, params = "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}
					}
					err := policyCall(t, mcp, method, params, nil)
					if public == "deny" {
						policyError(t, err, api.PolicyDenied)
					} else {
						id := policyError(t, err, api.PendingApproval).Data.ApprovalID
						a := policyGetApproval(t, person, id)
						if slices.Contains(a.Choices, "always") {
							t.Fatalf("public exposure offered persistent grant: %+v", a)
						}
						policyError(t, policyCall(t, person, "approval.decide", api.DecideApprovalParams{ID: id, Decision: "approve", Scope: "always", Code: a.Code}, nil), api.PolicyDenied)
					}
					policyNoHolders(t, person)
				})
			}
		}
	}
}

func TestPolicyIntegrationApprovalOnceAndPrivateViews(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	mcp := dialClient(t, socket, "mcp", "agent", "")
	person := dialClient(t, socket, "gui", "reviewer", "")
	params := api.OpenSavedParams{ID: "t", Purpose: "inspect the database"}
	id := policyPending(t, mcp, "tunnel.open", params)
	a := policyGetApproval(t, person, id)
	if a.Code == "" || len(a.Argv) == 0 {
		t.Fatalf("person get lacks approval details: %+v", a)
	}
	var personList []api.Approval
	policyOK(t, person, "approval.list", api.Empty{}, &personList)
	if len(personList) != 1 || personList[0].Code != a.Code || !slices.Equal(personList[0].Argv, a.Argv) {
		t.Fatalf("person list lacks approval details: %+v", personList)
	}
	var raw map[string]json.RawMessage
	policyOK(t, mcp, "approval.get", api.IDParams{ID: id}, &raw)
	for _, key := range []string{"code", "argv", "session"} {
		if _, exists := raw[key]; exists {
			t.Errorf("MCP get leaked %s: %s", key, raw[key])
		}
	}
	var mcpList []map[string]json.RawMessage
	policyOK(t, mcp, "approval.list", api.Empty{}, &mcpList)
	if len(mcpList) != 1 {
		t.Fatalf("MCP list = %+v", mcpList)
	}
	for _, key := range []string{"code", "argv", "session"} {
		if _, exists := mcpList[0][key]; exists {
			t.Errorf("MCP list leaked %s", key)
		}
	}
	policyError(t, policyCall(t, person, "approval.decide", api.DecideApprovalParams{ID: id, Decision: "approve", Scope: "once", Code: a.Code + "wrong"}, nil), api.PolicyDenied)
	// Even possession of the correct human code confers no MCP decision authority.
	policyError(t, policyCall(t, mcp, "approval.decide", api.DecideApprovalParams{ID: id, Decision: "approve", Scope: "once", Code: a.Code}, nil), api.PolicyDenied)
	if got := policyGetApproval(t, person, id).Status; got != "pending" {
		t.Fatalf("rejected decisions changed status to %s", got)
	}
	if got := policyPending(t, mcp, "tunnel.open", params); got != id {
		t.Fatalf("rejected decisions replaced pending request with %s", got)
	}
	policyDecide(t, person, id, "approve", "once")
	// A decision is not a deferred start. Only the owning agent's retry runs.
	policyNoHolders(t, person)
	policyStopped(t, person, "t")
	var opened string
	policyOK(t, mcp, "tunnel.open", params, &opened)
	var ready supervisor.Status
	policyOK(t, mcp, "tunnel.waitReady", api.IDParams{ID: "t"}, &ready)
	if ready.State != supervisor.StateRunning || ready.PID != 123 || opened != "t" || policyTunnel(t, person, "t").Owner.Holder == nil {
		t.Fatalf("approved retry did not run fake process: opened=%q status=%+v", opened, ready)
	}
	if got := policyGetApproval(t, person, id).Status; got != "consumed" {
		t.Fatalf("once grant status = %s", got)
	}
	policyOK(t, mcp, "tunnel.release", api.IDParams{ID: "t"}, nil)
	if again := policyPending(t, mcp, "tunnel.open", params); again == id {
		t.Fatal("consumed once grant authorized a second operation")
	}
	policyNoHolders(t, person)
}

func TestPolicyIntegrationSavedFingerprintChangesRequireNewApproval(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	mcp := dialClient(t, socket, "mcp", "agent", "")
	person := dialClient(t, socket, "gui", "editor", "")
	params := api.OpenSavedParams{ID: "t"}
	id := policyPending(t, mcp, "tunnel.open", params)
	before := policyGetApproval(t, person, id)
	policyDecide(t, person, id, "approve", "once")
	changed := policyTunnel(t, person, "t").Tunnel
	changed.SSH.Hostname = "different.example.invalid"
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: changed}, nil)
	next := policyPending(t, mcp, "tunnel.open", params)
	if next == id {
		t.Fatal("changed saved spec reused the approved request")
	}
	after := policyGetApproval(t, person, next)
	if after.Fingerprint == before.Fingerprint || slices.Equal(after.Argv, before.Argv) || after.Status != "pending" {
		t.Fatalf("new saved spec not fingerprint-bound: before=%+v after=%+v", before, after)
	}
	policyNoHolders(t, person)
	policyStopped(t, person, "t")
}

func TestPolicyIntegrationDeniedAndTimedOutApprovalsCannotRun(t *testing.T) {
	for _, outcome := range []string{"deny", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			s, socket, clock := policyServer(t, settings.StrictAgent(), nil)
			mcp := dialClient(t, socket, "mcp", "agent", "")
			person := dialClient(t, socket, "cli", "reviewer", "")
			params := api.OpenSavedParams{ID: "t"}
			id := policyPending(t, mcp, "tunnel.open", params)
			want := "denied"
			if outcome == "deny" {
				policyDecide(t, person, id, "deny", "once")
			} else {
				clock.advance(engine.ApprovalTimeout - time.Nanosecond)
				if err := s.engine.Maintain(); err != nil {
					t.Fatal(err)
				}
				if got := policyGetApproval(t, person, id).Status; got != "pending" {
					t.Fatalf("approval expired before deadline: %s", got)
				}
				clock.advance(time.Nanosecond)
				if err := s.engine.Maintain(); err != nil {
					t.Fatal(err)
				}
				want = "timed_out"
			}
			a := policyGetApproval(t, person, id)
			if a.Status != want {
				t.Fatalf("status = %s, want %s", a.Status, want)
			}
			policyError(t, policyCall(t, person, "approval.decide", api.DecideApprovalParams{ID: id, Decision: "approve", Scope: "once", Code: a.Code}, nil), api.PolicyDenied)
			for i := 0; i < 2; i++ {
				policyError(t, policyCall(t, mcp, "tunnel.open", params, nil), api.PolicyDenied)
			}
			var approvals []api.Approval
			policyOK(t, person, "approval.list", api.Empty{}, &approvals)
			if len(approvals) != 1 {
				t.Fatalf("denied retry minted another request: %+v", approvals)
			}
			policyNoHolders(t, person)
			policyStopped(t, person, "t")
		})
	}
}

func TestPolicyIntegrationSessionGrantAndApprovalIsolation(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	first := dialClient(t, socket, "mcp", "same-name", "")
	person := dialClient(t, socket, "gui", "reviewer", "")
	params := api.OpenSavedParams{ID: "t"}
	id := policyPending(t, first, "tunnel.open", params)
	policyDecide(t, person, id, "approve", "session")
	for i := 0; i < 2; i++ {
		var opened string
		policyOK(t, first, "tunnel.open", params, &opened)
		if opened != "t" {
			t.Fatalf("grant opened wrong tunnel: %q", opened)
		}
	}
	resumed := dialClient(t, socket, "mcp", "same-name", first.Session())
	policyOK(t, resumed, "tunnel.open", params, nil)
	if got := policyGetApproval(t, person, id).Status; got != "approved" {
		t.Fatalf("session grant was consumed: %s", got)
	}
	for _, name := range []string{"same-name", "other-name"} {
		other := dialClient(t, socket, "mcp", name, "")
		if other.Session() == first.Session() {
			t.Fatal("different connection implicitly reused session")
		}
		policyError(t, policyCall(t, other, "approval.get", api.IDParams{ID: id}, nil), api.Unknown)
		var approvals []api.Approval
		policyOK(t, other, "approval.list", api.Empty{}, &approvals)
		if len(approvals) != 0 {
			t.Fatalf("another session saw approvals: %+v", approvals)
		}
		next := policyPending(t, other, "tunnel.open", params)
		if next == id {
			t.Fatal("session grant escaped to another session")
		}
		policyError(t, policyCall(t, first, "approval.get", api.IDParams{ID: next}, nil), api.Unknown)
		policyOK(t, other, "approval.list", api.Empty{}, &approvals)
		if len(approvals) != 1 || approvals[0].ID != next {
			t.Fatalf("session list did not isolate own request: %+v", approvals)
		}
	}
}

func TestPolicyIntegrationAliasMustExistAndNewSecretsAsk(t *testing.T) {
	for _, tc := range []struct {
		name   string
		known  bool
		secret bool
		allow  bool
	}{
		{"known allowlisted alias", true, false, true},
		{"missing allowlisted alias", false, false, false},
		{"new secret reference on allowed alias", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := settings.StrictAgent()
			agent.AllowSSHHosts = []string{"bastion"}
			_, socket, _ := policyServer(t, agent, func(opts *engine.Options, cfg *model.Config) {
				path := filepath.Join(filepath.Dir(opts.ConfigPath), ".ssh", "config")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				contents := "# No alias definitions\n"
				if tc.known {
					contents = "Host bastion\n  HostName bastion.example.invalid\n"
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
				_ = cfg
			})
			mcp := dialClient(t, socket, "mcp", "alias-agent", "")
			person := dialClient(t, socket, "gui", "person", "")
			tunnel := model.Tunnel{
				Name: "Alias tunnel", Type: model.TypeSSH,
				SSH:      &model.SSH{Host: "bastion", Flags: []string{"-N"}},
				Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:24322", Target: "127.0.0.1:5432"}},
			}
			if tc.secret {
				tunnel.SSH.PassphraseSecret = "existing-key-passphrase"
				policyOK(t, person, "secret.set", api.SetSecretParams{
					Name: tunnel.SSH.PassphraseSecret, Value: "test-only-passphrase", Kind: secret.KindSSHPassphrase,
				}, nil)
			}
			params := api.RunEphemeralParams{Tunnel: tunnel}
			var result api.RunEphemeralResult
			err := policyCall(t, mcp, "tunnel.runEphemeral", params, &result)
			if tc.allow {
				if err != nil || result.ID == "" || policyTunnel(t, person, result.ID).Owner.Holder == nil {
					t.Fatalf("known alias was not allowed: %+v %v", result, err)
				}
				policyOK(t, mcp, "tunnel.release", api.IDParams{ID: result.ID}, nil)
				return
			}
			id := policyError(t, err, api.PendingApproval).Data.ApprovalID
			if a := policyGetApproval(t, person, id); slices.Contains(a.Choices, "always") {
				t.Fatalf("unknown alias or new secret offered always: %+v", a)
			}
			policyNoHolders(t, person)
		})
	}
}

func TestPolicyIntegrationSaveNeedsSeparateApprovalAndCannotEditOrStart(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	mcp := dialClient(t, socket, "mcp", "save-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
	draft := policySSH("")
	draft.Name = "Agent saved"
	params := api.SaveTunnelParams{Tunnel: draft, Purpose: "remember this tunnel"}
	id := policyPending(t, mcp, "tunnel.save", params)
	if a := policyGetApproval(t, person, id); slices.Contains(a.Choices, "always") {
		t.Fatalf("save offered persistent approval: %+v", a)
	}
	var tunnels []api.TunnelView
	policyOK(t, person, "tunnel.list", api.Empty{}, &tunnels)
	if len(tunnels) != 1 {
		t.Fatalf("pending save changed registry: %+v", tunnels)
	}
	policyDecide(t, person, id, "approve", "once")
	withStart := params
	withStart.Start = true
	policyError(t, policyCall(t, mcp, "tunnel.save", withStart, nil), api.PolicyDenied)
	edit := policyTunnel(t, person, "t").Tunnel
	edit.SSH.Options = map[string]string{"ProxyCommand": "echo never-execute"}
	policyError(t, policyCall(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: edit}, nil), api.PolicyDenied)
	if got := policyGetApproval(t, person, id).Status; got != "approved" {
		t.Fatalf("forbidden save variation consumed unrelated approval: %s", got)
	}
	var savedID string
	policyOK(t, mcp, "tunnel.save", params, &savedID)
	if savedID == "" || savedID == "t" {
		t.Fatalf("save result = %q", savedID)
	}
	policyStopped(t, person, savedID)
	policyNoHolders(t, person)
	policyError(t, policyCall(t, mcp, "tunnel.start", api.IDParams{ID: savedID}, nil), api.PolicyDenied)
	policyPending(t, mcp, "tunnel.open", api.OpenSavedParams{ID: savedID})
	if got := policyGetApproval(t, person, id).Status; got != "consumed" {
		t.Fatalf("save grant was not consumed: %s", got)
	}
	if got := policyTunnel(t, person, "t").Tunnel.SSH.Options; len(got) != 0 {
		t.Fatalf("forbidden saved edit took effect: %v", got)
	}
}

func TestPolicyIntegrationPromoteNeedsOwnTunnelAndSeparateApproval(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	mcp := dialClient(t, socket, "mcp", "promote-agent", "")
	other := dialClient(t, socket, "mcp", "promote-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
	params := api.RunEphemeralParams{Tunnel: policySSH("")}
	runApproval := policyPending(t, mcp, "tunnel.runEphemeral", params)
	policyDecide(t, person, runApproval, "approve", "once")
	var run api.RunEphemeralResult
	policyOK(t, mcp, "tunnel.runEphemeral", params, &run)
	var ready supervisor.Status
	policyOK(t, mcp, "tunnel.waitReady", api.IDParams{ID: run.ID}, &ready)
	promote := api.PromoteTunnelParams{ID: run.ID, SavedID: "promoted", Name: "Promoted by agent"}
	policyError(t, policyCall(t, other, "tunnel.promote", promote, nil), api.PolicyDenied)
	id := policyPending(t, mcp, "tunnel.promote", promote)
	if id == runApproval {
		t.Fatal("run approval authorized promotion")
	}
	if a := policyGetApproval(t, person, id); slices.Contains(a.Choices, "always") {
		t.Fatalf("promotion offered persistent approval: %+v", a)
	}
	if view := policyTunnel(t, person, run.ID); !view.Owner.Ephemeral {
		t.Fatal("pending promotion already saved the tunnel")
	}
	policyDecide(t, person, id, "approve", "once")
	var savedID string
	policyOK(t, mcp, "tunnel.promote", promote, &savedID)
	view := policyTunnel(t, person, savedID)
	if savedID != "promoted" || view.Owner.Ephemeral || view.Status.PID != ready.PID || view.Status.State != supervisor.StateRunning {
		t.Fatalf("promotion did not preserve process: before=%+v after=%+v", ready, view)
	}
	if view.Owner.Holder == nil || view.Owner.Holder.Name != "promote-agent" {
		t.Fatalf("promotion lost the holder: %+v", view.Owner)
	}
	policyError(t, policyCall(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: view.Tunnel}, nil), api.PolicyDenied)
	policyError(t, policyCall(t, mcp, "tunnel.start", api.IDParams{ID: savedID}, nil), api.PolicyDenied)
}

func TestPolicyIntegrationDisabledDeniesAllAgentMethods(t *testing.T) {
	agent := settings.StrictAgent()
	agent.Enabled = false
	agent.AllowTunnels = []string{"*"}
	agent.AllowSSHHosts = []string{"bastion"}
	_, socket, _ := policyServer(t, agent, nil)
	mcp := dialClient(t, socket, "mcp", "disabled-agent", "")
	person := dialClient(t, socket, "gui", "person", "")
	// Create an actual promotion target as a person so refusal cannot be
	// confused with a nonexistent-ID error.
	var run api.RunEphemeralResult
	policyOK(t, person, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: policySSH("")}, &run)
	for _, tc := range []struct {
		method string
		params any
	}{
		{"tunnel.open", api.OpenSavedParams{ID: "t"}},
		{"tunnel.release", api.IDParams{ID: run.ID}},
		{"tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: policySSH("")}},
		{"tunnel.save", api.SaveTunnelParams{Tunnel: policySSH("")}},
		{"tunnel.promote", api.PromoteTunnelParams{ID: run.ID, SavedID: "disabled-promote"}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			policyError(t, policyCall(t, mcp, tc.method, tc.params, nil), api.AgentDisabled)
		})
	}
	var approvals []api.Approval
	policyOK(t, person, "approval.list", api.Empty{}, &approvals)
	if len(approvals) != 0 {
		t.Fatalf("disabled mutation queued approval: %+v", approvals)
	}
	policyStopped(t, person, "t")
	// The master switch protects inspection too, independently of the allow-list.
	for _, method := range api.Methods {
		if method == api.DaemonHello || method == api.DaemonStatusMethod || method == "policy.get" {
			continue
		}
		policyError(t, policyCall(t, mcp, method, api.Empty{}, nil), api.AgentDisabled)
	}
	policyOK(t, mcp, api.DaemonStatusMethod, api.Empty{}, nil)
	policyOK(t, mcp, "policy.get", api.Empty{}, nil)
}

func TestPolicyIntegrationConstantMethodAllowList(t *testing.T) {
	// Deliberately literal, independent of handlers, readOnly and api.Methods.
	// New API registrations must stay denied until this security contract is
	// consciously changed. daemon.hello is covered by dialClient's initial
	// handshake; replaying it is not an allow-list probe.
	allowed := map[string]bool{
		"daemon.hello": true, "daemon.status": true, "events.subscribe": true,
		"tunnel.openRequest": true, "tunnel.waitRequest": true, "tunnel.checkRequest": true, "policy.get": true,
		"tunnel.list": true, "tunnel.get": true, "tunnel.waitReady": true,
		"tunnel.findings": true, "tunnel.evaluateDraft": true,
		"ssh.hosts": true, "ssh.resolve": true, "tool.list": true, "prompt.list": true,
		"tunnel.open": true, "tunnel.release": true,
		"tunnel.runEphemeral": true, "approval.list": true, "approval.get": true,
		"logs.read": true, "logs.since": true, "tunnel.promote": true, "tunnel.save": true,
	}
	s, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	// Model a future method registered as read-only: neither registration nor
	// that classification may implicitly grant MCP access.
	future := "future.readSensitiveState"
	query(s, future, func(api.Empty) string { return "must-not-be-reachable" })
	mcp := dialClient(t, socket, "mcp", "allowlist-agent", "")
	if mcp.Session() == "" {
		t.Fatal("initial daemon.hello did not establish an MCP session")
	}
	seen := map[string]bool{}
	methods := append(slices.Clone(api.Methods), future, "future.unregisteredMutation")
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			seen[method] = true
			if got := mcpMethodAllowed(method); got != allowed[method] {
				t.Errorf("MCP allow-list membership = %v, want literal expectation %v", got, allowed[method])
			}
			if !allowed[method] {
				policyError(t, policyCall(t, mcp, method, api.Empty{}, nil), api.PolicyDenied)
			}
		})
	}
	for method := range allowed {
		if !seen[method] {
			t.Errorf("expected MCP method missing from API contract: %s", method)
		}
	}
	policyStopped(t, mcp, "t")
	policyNoHolders(t, mcp)
}

func TestHelloRejectsUnknownClientKinds(t *testing.T) {
	_, socket, _ := policyServer(t, settings.StrictAgent(), nil)
	for _, kind := range []string{"MCP", "Mcp", "agent", "gui "} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: kind, Name: "x"})
		cancel()
		if err == nil {
			c.Close()
			t.Fatalf("hello with kind %q was accepted", kind)
		}
	}
}
