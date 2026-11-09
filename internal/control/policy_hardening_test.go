package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func policyHostConfig(t *testing.T, opts *engine.Options, text string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(opts.ConfigPath), ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMCPRejectsPersistenceMetadata(t *testing.T) {
	for _, field := range []string{"startOnLaunch", "profile", "groups", "favorite"} {
		t.Run(field, func(t *testing.T) {
			_, socket, _ := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
				cfg.Profiles = []model.Profile{{ID: "work", Name: "Work"}}
				cfg.Groups = []model.Group{{ID: "stack", Name: "Stack"}}
			})
			mcp := leasedDial(t, socket, "mcp", "metadata-agent", "")
			person := leasedDial(t, socket, "gui", "person", "")
			base := policySSH("")
			p := api.SaveTunnelParams{Tunnel: base}
			id := policyPending(t, mcp, "tunnel.save", p)
			policyDecide(t, person, id, "approve", "once")
			switch field {
			case "startOnLaunch":
				p.Tunnel.StartOnLaunch = true
			case "profile":
				p.Tunnel.Profile = "work"
			case "groups":
				p.Tunnel.Groups = []string{"stack"}
			case "favorite":
				p.Tunnel.Favorite = true
			}
			for _, method := range []string{"tunnel.save", "tunnel.runEphemeral"} {
				var params any = p
				if method == "tunnel.runEphemeral" {
					params = api.RunEphemeralParams{Tunnel: p.Tunnel}
				}
				err := policyError(t, policyCall(t, mcp, method, params, nil), api.PolicyDenied)
				if !strings.Contains(err.Message, "startOnLaunch, profile, groups or favorite") {
					t.Fatalf("unclear denial: %v", err)
				}
			}
			if got := policyGetApproval(t, person, id).Status; got != "approved" {
				t.Fatalf("denied metadata consumed approval: %s", got)
			}
			var saved string
			policyOK(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: base}, &saved)
			policyOK(t, person, "tunnel.startOnLaunch", api.Empty{}, nil)
			policyStopped(t, person, saved)
			policyNoLeases(t, person)
		})
	}
}

func TestPersonBulkStartsSkipEphemeralTunnels(t *testing.T) {
	s, socket, clock := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
		cfg.Profiles = []model.Profile{{ID: "work", Name: "Work"}}
		cfg.Groups = []model.Group{{ID: "stack", Name: "Stack"}}
	})
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := policySSH("")
	tunnel.StartOnLaunch, tunnel.Favorite, tunnel.Profile, tunnel.Groups = true, true, "work", []string{"stack"}
	var result api.RunEphemeralResult
	policyOK(t, person, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel, TTL: "1m"}, &result)
	policyOK(t, person, "tunnel.waitReady", api.IDParams{ID: result.ID}, nil)
	policyOK(t, person, "tunnel.startOnLaunch", api.Empty{}, nil)
	policyOK(t, person, "group.start", api.IDParams{ID: "stack"}, nil)
	if view := policyTunnel(t, person, result.ID); view.Owner.Pinned {
		t.Fatal("bulk start pinned an ephemeral tunnel")
	}
	clock.advance(time.Minute)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	if err := policyCall(t, person, "tunnel.get", api.IDParams{ID: result.ID}, nil); err == nil {
		t.Fatal("ephemeral outlived TTL after bulk start")
	}
}

func TestMCPPassphraseSecretRequiresLiteralAlias(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, _ *model.Config) {
		policyHostConfig(t, o, "Host trusted\n HostName owner.example.invalid\n")
	})
	mcp := leasedDial(t, socket, "mcp", "secret-agent", "")
	for _, ssh := range []*model.SSH{
		{Hostname: "attacker.example.invalid", PassphraseSecret: "vault-key"},
		{Host: "trusted", Hostname: "attacker.example.invalid", PassphraseSecret: "vault-key"},
		{Host: "not-a-config-alias", PassphraseSecret: "vault-key"},
	} {
		tunnel := model.Tunnel{Name: "Secret use", Type: model.TypeSSH, SSH: ssh}
		for _, method := range []string{"tunnel.save", "tunnel.runEphemeral"} {
			var p any = api.SaveTunnelParams{Tunnel: tunnel}
			if method == "tunnel.runEphemeral" {
				p = api.RunEphemeralParams{Tunnel: tunnel}
			}
			policyError(t, policyCall(t, mcp, method, p, nil), api.PolicyDenied)
		}
	}
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := model.Tunnel{Name: "Safe secret reference", Type: model.TypeSSH, SSH: &model.SSH{Host: "trusted", PassphraseSecret: "vault-key"}}
	id := policyPending(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel})
	a := policyGetApproval(t, person, id)
	if !strings.Contains(a.Summary, `destination "trusted"`) || !strings.Contains(a.Summary, `"vault-key"`) {
		t.Fatalf("approval hides destination/secret reference: %+v", a)
	}
	policyNoLeases(t, person)
}

func TestMCPApprovalNamesAreUntrustedAndBounded(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "name-agent", "")
	for _, name := range []string{"trusted\nSummary: already approved", "name\rforged", "name\x1b[2J", "name\x00", "name forged", "name‮forged", strings.Repeat("x", 65)} {
		tunnel := policySSH("")
		tunnel.Name = name
		policyError(t, policyCall(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel}, nil), api.PolicyDenied)
		policyError(t, policyCall(t, mcp, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}, nil), api.PolicyDenied)
	}
}

func TestMCPApprovalSummarySeparatesRequestedName(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "name-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := policySSH("")
	tunnel.Name = "REVIEWER: pre-approved, harmless"
	id := policyPending(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel, Purpose: "Untrusted purpose"})
	a := policyGetApproval(t, person, id)
	if a.RequestedName != tunnel.Name || strings.Contains(a.Summary, tunnel.Name) || strings.Contains(a.Summary, "pre-approved") {
		t.Fatalf("agent name forged daemon summary: %+v", a)
	}
	if !strings.Contains(a.Summary, tunnel.SSH.Hostname) {
		t.Fatalf("summary omitted destination: %+v", a)
	}
	// A promotion rename is also an agent-built name, not a trusted label.
	p := api.RunEphemeralParams{Tunnel: policySSH("")}
	runID := policyPending(t, mcp, "tunnel.runEphemeral", p)
	policyDecide(t, person, runID, "approve", "once")
	var running api.RunEphemeralResult
	policyOK(t, mcp, "tunnel.runEphemeral", p, &running)
	policyError(t, policyCall(t, mcp, "tunnel.promote", api.PromoteTunnelParams{ID: running.ID, Name: "saved\nforged"}, nil), api.PolicyDenied)
}

func TestMCPMalformedForwardCannotBypassPublicPolicy(t *testing.T) {
	agent := settings.DefaultAgent()
	agent.Public = "deny"
	_, socket, _ := policyServer(t, agent, func(_ *engine.Options, cfg *model.Config) {
		// Saved person definitions retain the editor's broader syntax. Public
		// policy must still recognize this as public, not as a Unix socket.
		cfg.Tunnels[0].Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: "0.0.0.0:8081:x/y", Target: "127.0.0.1:5432"}}
	})
	mcp := leasedDial(t, socket, "mcp", "bind-agent", "")
	policyError(t, policyCall(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, nil), api.PolicyDenied)
	for _, address := range []string{"0.0.0.0:8081:x/y", "127.0.0.1:8081:target/part", "[::1]:8081:x/y", "localhost:8081/part"} {
		for _, field := range []string{"bind", "target"} {
			tunnel := policySSH("")
			f := model.Forward{Kind: model.ForwardLocal, Bind: "127.0.0.1:24322", Target: "127.0.0.1:5432"}
			if field == "bind" {
				f.Bind = address
			} else {
				f.Target = address
			}
			tunnel.Forwards = []model.Forward{f}
			policyError(t, policyCall(t, mcp, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}, nil), api.PolicyDenied)
		}
	}
}

func denialEvents(t *testing.T, person *client.Client) []audit.Event {
	t.Helper()
	var batch api.AuditReadResult
	policyOK(t, person, "audit.read", api.AuditReadParams{Limit: 1000}, &batch)
	var out []audit.Event
	for _, event := range batch.Events {
		if event.Action == audit.PolicyDeny {
			out = append(out, event)
		}
	}
	return out
}

func TestMCPDenialAuditsAreCoalescedPerSession(t *testing.T) {
	s, socket, clock := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "flood-agent", "")
	resumed := leasedDial(t, socket, "mcp", "flood-agent", mcp.Session())
	person := leasedDial(t, socket, "gui", "person", "")
	const calls = 80
	for i := 0; i < calls; i++ {
		c := mcp
		if i%2 == 1 {
			c = resumed
		}
		if i%3 == 0 {
			tunnel := policySSH("")
			tunnel.Favorite = true
			policyError(t, policyCall(t, c, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel}, nil), api.PolicyDenied)
		} else {
			policyError(t, policyCall(t, c, "secret.list", api.Empty{}, nil), api.PolicyDenied)
		}
	}
	if got := len(denialEvents(t, person)); got != engine.MaxDenialAudits {
		t.Fatalf("flood wrote %d audit events, want %d", got, engine.MaxDenialAudits)
	}
	other := leasedDial(t, socket, "mcp", "other-agent", "")
	policyError(t, policyCall(t, other, "secret.list", api.Empty{}, nil), api.PolicyDenied)
	if got := len(denialEvents(t, person)); got != engine.MaxDenialAudits+1 {
		t.Fatalf("other session lost its audit budget: %d", got)
	}
	clock.advance(time.Minute)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	events := denialEvents(t, person)
	if len(events) != engine.MaxDenialAudits+2 || events[len(events)-1].Count != calls-engine.MaxDenialAudits {
		t.Fatalf("no accurate end-of-window summary: %+v", events)
	}
	policyError(t, policyCall(t, mcp, "secret.list", api.Empty{}, nil), api.PolicyDenied)
	if got := len(denialEvents(t, person)); got != len(events)+1 {
		t.Fatalf("new window has no audit budget: %d", got)
	}
}

func TestMCPPendingApprovalCapIsPerSession(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
		for i := 0; i <= engine.MaxPendingApprovalsPerSession; i++ {
			cfg.Tunnels = append(cfg.Tunnels, policySSH(fmt.Sprintf("saved-%d", i)))
		}
	})
	mcp := leasedDial(t, socket, "mcp", "queue-agent", "")
	resumed := leasedDial(t, socket, "mcp", "queue-agent", mcp.Session())
	for i := 0; i < engine.MaxPendingApprovalsPerSession; i++ {
		policyPending(t, mcp, "lease.open", api.OpenLeaseParams{ID: fmt.Sprintf("saved-%d", i)})
	}
	err := policyError(t, policyCall(t, resumed, "lease.open", api.OpenLeaseParams{ID: "t"}, nil), api.PolicyDenied)
	if !strings.Contains(err.Message, "16 pending approvals") {
		t.Fatalf("unclear queue cap: %v", err)
	}
	other := leasedDial(t, socket, "mcp", "queue-agent", "")
	policyPending(t, other, "lease.open", api.OpenLeaseParams{ID: "t"})
}

func TestMCPCompletedApprovalsPrunedWhileConnected(t *testing.T) {
	s, socket, clock := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
		cfg.Tunnels = append(cfg.Tunnels, policySSH("denied"), policySSH("timeout"))
	})
	mcp := leasedDial(t, socket, "mcp", "retained-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	consumed := policyPending(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"})
	policyDecide(t, person, consumed, "approve", "once")
	policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, nil)
	denied := policyPending(t, mcp, "lease.open", api.OpenLeaseParams{ID: "denied"})
	policyDecide(t, person, denied, "deny", "once")
	timed := policyPending(t, mcp, "lease.open", api.OpenLeaseParams{ID: "timeout"})
	clock.advance(engine.ApprovalTimeout)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{consumed, denied} {
		policyError(t, policyCall(t, mcp, "approval.get", api.IDParams{ID: id}, nil), api.Unknown)
	}
	if a := policyGetApproval(t, mcp, timed); a.Status != "timed_out" {
		t.Fatalf("not timed out: %+v", a)
	}
	clock.advance(engine.ApprovalRetention)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	policyError(t, policyCall(t, mcp, "approval.get", api.IDParams{ID: timed}, nil), api.Unknown)
	var approvals []api.Approval
	policyOK(t, person, "approval.list", api.Empty{}, &approvals)
	if len(approvals) != 0 {
		t.Fatalf("terminal approvals retained forever: %+v", approvals)
	}
}

type policyCountingStore struct {
	secret.Memory
	reads atomic.Int64
}

func (s *policyCountingStore) Get(name string) (string, error) {
	s.reads.Add(1)
	return s.Memory.Get(name)
}

func TestMCPDraftDoesNotProbeSecretPresence(t *testing.T) {
	store := &policyCountingStore{}
	if err := store.Set("present-name", "never-disclose"); err != nil {
		t.Fatal(err)
	}
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, cfg *model.Config) {
		o.Secrets = store
		cfg.Tunnels[0].SSH.PassphraseSecret = "saved-secret"
	})
	mcp := leasedDial(t, socket, "mcp", "probe-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	policyOK(t, person, "secret.forget", api.Empty{}, nil)
	before := store.reads.Load()
	for _, name := range []string{"present-name", "absent-name"} {
		tunnel := policySSH("")
		tunnel.SSH.PassphraseSecret = name
		var result api.DraftResult
		policyOK(t, mcp, "tunnel.evaluateDraft", api.TunnelParams{Tunnel: tunnel}, &result)
		for _, f := range result.Findings {
			if f.Field == "secret" || strings.Contains(f.Message, name) {
				t.Fatalf("secret presence leaked: %+v", result)
			}
		}
	}
	var findings []checks.Finding
	policyOK(t, mcp, "tunnel.findings", api.IDParams{ID: "t"}, &findings)
	for _, f := range findings {
		if f.Field == "secret" {
			t.Fatalf("saved findings bypassed redaction: %+v", findings)
		}
	}
	if got := store.reads.Load(); got != before {
		t.Fatalf("MCP dry run read keychain %d times", got-before)
	}
	// Person checks retain their existing presence diagnostics.
	var personResult api.DraftResult
	tunnel := policySSH("")
	tunnel.SSH.PassphraseSecret = "absent-person-secret"
	policyOK(t, person, "tunnel.evaluateDraft", api.TunnelParams{Tunnel: tunnel}, &personResult)
	found := false
	for _, f := range personResult.Findings {
		found = found || f.Field == "secret"
	}
	if !found || store.reads.Load() == before {
		t.Fatal("person secret checks changed")
	}
}

func TestMCPPrintableClientNameAuditsMutations(t *testing.T) {
	agent := settings.DefaultAgent()
	agent.AllowTunnels = []string{"t"}
	_, socket, _ := policyServer(t, agent, nil)
	mcp := leasedDial(t, socket, "mcp", "Claude Desktop (local)", "")
	person := leasedDial(t, socket, "gui", "person", "")
	var lease api.Lease
	policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, &lease)
	var batch api.AuditReadResult
	policyOK(t, person, "audit.read", api.AuditReadParams{Actor: "Claude Desktop (local)"}, &batch)
	if len(batch.Events) != 1 || batch.Events[0].Action != audit.LeaseOpen {
		t.Fatalf("printable name broke audit: %+v", batch)
	}
	for _, name := range []string{"bad\nname", "bad\x00name", "bad\x1bname", "bad‮name", strings.Repeat("x", 257)} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: "mcp", Name: name})
		cancel()
		if err == nil {
			c.Close()
			t.Fatalf("nonprintable/oversize actor accepted: %q", name)
		}
	}
}

func TestMCPTunnelReadsHideExecutionAndCredentials(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
		ssh := policySSH("ssh-private")
		ssh.SSH.Identity = "/private/identity-sentinel"
		ssh.SSH.ExtraArgs = "-v extra-sentinel"
		ssh.SSH.Options = map[string]string{"ProxyCommand": "option-sentinel"}
		ssh.SSH.PassphraseSecret = "passphrase-reference-sentinel"
		cfg.Tunnels = []model.Tunnel{ssh,
			{ID: "command-private", Name: "Command", Type: model.TypeCommand, Command: &model.Command{Line: "echo command-sentinel", Dir: "directory-sentinel", ReadyLog: "ready-sentinel", SecretEnvs: map[string]string{"ENV_SENTINEL": "env-reference-sentinel"}}},
			{ID: "ngrok-private", Name: "Ngrok", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "3000", ExtraArgs: "ngrok-extra-sentinel", Secret: "ngrok-reference-sentinel"}},
			{ID: "cloud-private", Name: "Cloud", Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000", ExtraArgs: "cloud-extra-sentinel", Secret: "cloud-reference-sentinel"}},
		}
	})
	mcp := leasedDial(t, socket, "mcp", "reader-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	var views []api.TunnelView
	policyOK(t, mcp, "tunnel.list", api.Empty{}, &views)
	if len(views) != 4 {
		t.Fatalf("lost tunnel identities: %+v", views)
	}
	for _, v := range views {
		for _, method := range []string{"tunnel.list", "tunnel.get"} {
			var raw json.RawMessage
			var p any = api.Empty{}
			if method == "tunnel.get" {
				p = api.IDParams{ID: v.Tunnel.ID}
			}
			policyOK(t, mcp, method, p, &raw)
			if strings.Contains(string(raw), "sentinel") {
				t.Fatalf("%s leaked saved execution/credential fields: %s", method, raw)
			}
		}
		var got api.TunnelView
		policyOK(t, mcp, "tunnel.get", api.IDParams{ID: v.Tunnel.ID}, &got)
		if got.Tunnel.ID != v.Tunnel.ID || got.Tunnel.Name != v.Tunnel.Name || got.Preview != "" || len(got.MissingSecrets) != 0 {
			t.Fatalf("bad redacted view: %+v", got)
		}
	}
	var raw json.RawMessage
	policyOK(t, person, "tunnel.list", api.Empty{}, &raw)
	for _, want := range []string{"identity-sentinel", "command-sentinel", "option-sentinel", "env-reference-sentinel", "ngrok-extra-sentinel", "cloud-reference-sentinel"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("person lost saved definition %s", want)
		}
	}
}

func TestMCPSSHResolveRequiresListedLiteralAlias(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, _ *model.Config) {
		policyHostConfig(t, o, "Host trusted\n HostName owner.example.invalid\nHost *.wild.invalid\n HostName wildcard.invalid\n")
	})
	mcp := leasedDial(t, socket, "mcp", "resolve-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	for _, alias := range []string{"manual.invalid", "one.wild.invalid", "-Fbad", "host with space"} {
		policyError(t, policyCall(t, mcp, "ssh.resolve", api.SSHParams{Alias: alias}, nil), api.PolicyDenied)
	}
	// The fake SSH executable is the test binary, so it may return an execution
	// error. The admission boundary must nevertheless accept the listed alias.
	var resolved sshconfig.Resolved
	err := policyCall(t, mcp, "ssh.resolve", api.SSHParams{Alias: "trusted"}, &resolved)
	if err != nil && strings.Contains(err.Error(), "agent policy") {
		t.Fatalf("listed literal alias denied: %v", err)
	}
	// Person resolution remains unrestricted (also routed to the fake tool).
	err = policyCall(t, person, "ssh.resolve", api.SSHParams{Alias: "manual.invalid"}, &resolved)
	if err != nil && strings.Contains(err.Error(), "agent policy") {
		t.Fatalf("person resolution restricted: %v", err)
	}
}

func TestMCPAuditFailureCannotCommitRenewSaveOrPromote(t *testing.T) {
	for _, method := range []string{"lease.renew", "tunnel.save", "tunnel.promote", "person save"} {
		t.Run(method, func(t *testing.T) {
			agent := settings.DefaultAgent()
			agent.AllowTunnels = []string{"t"}
			var trail *audit.Writer
			var configPath, runtimePath string
			_, socket, clock := policyServer(t, agent, func(o *engine.Options, _ *model.Config) {
				configPath, runtimePath = o.ConfigPath, filepath.Join(o.StateDir, "runtime.json")
				var err error
				trail, err = audit.Open(o.StateDir, audit.Options{})
				if err != nil {
					t.Fatal(err)
				}
				o.Audit = trail
				t.Cleanup(func() { trail.Close() })
			})
			mcp := leasedDial(t, socket, "mcp", "audit-agent", "")
			person := leasedDial(t, socket, "gui", "person", "")
			var params any
			var lease api.Lease
			rpcMethod, caller := method, mcp
			switch method {
			case "person save":
				rpcMethod, caller = "tunnel.save", person
				params = api.SaveTunnelParams{Tunnel: policySSH("")}
			case "lease.renew":
				policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t", TTL: "5m"}, &lease)
				clock.advance(time.Second)
				params = api.RenewLeaseParams{ID: lease.ID, TTL: "10m"}
			case "tunnel.save":
				params = api.SaveTunnelParams{Tunnel: policySSH("")}
				id := policyPending(t, mcp, method, params)
				policyDecide(t, person, id, "approve", "once")
			case "tunnel.promote":
				p := api.RunEphemeralParams{Tunnel: policySSH("")}
				id := policyPending(t, mcp, "tunnel.runEphemeral", p)
				policyDecide(t, person, id, "approve", "once")
				var running api.RunEphemeralResult
				policyOK(t, mcp, "tunnel.runEphemeral", p, &running)
				policyOK(t, mcp, "tunnel.waitReady", api.IDParams{ID: running.ID}, nil)
				params = api.PromoteTunnelParams{ID: running.ID, SavedID: "promoted"}
				id = policyPending(t, mcp, method, params)
				policyDecide(t, person, id, "approve", "once")
			}
			beforeConfig, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeRuntime, _ := os.ReadFile(runtimePath)
			if err := trail.Close(); err != nil {
				t.Fatal(err)
			}
			if err := policyCall(t, caller, rpcMethod, params, nil); err == nil {
				t.Fatal("mutation succeeded without audit admission")
			}
			afterConfig, _ := os.ReadFile(configPath)
			afterRuntime, _ := os.ReadFile(runtimePath)
			if string(beforeConfig) != string(afterConfig) || string(beforeRuntime) != string(afterRuntime) {
				t.Fatalf("%s committed before refusing failed audit", method)
			}
			if method == "lease.renew" {
				var leases []api.Lease
				policyOK(t, mcp, "lease.list", api.Empty{}, &leases)
				if len(leases) != 1 || !leases[0].ExpiresAt.Equal(lease.ExpiresAt) {
					t.Fatalf("failed renewal changed in-memory expiry: %+v", leases)
				}
			}
		})
	}
}

func TestMCPExplicitBindCannotSquatSavedListenPort(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(_ *engine.Options, cfg *model.Config) {
		cfg.Tunnels[0].Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:24322", Target: "127.0.0.1:5432"}}
	})
	mcp := leasedDial(t, socket, "mcp", "squat-agent", "")
	for _, bind := range []string{"24322", "127.0.0.1:24322", "0.0.0.0:24322", "[::1]:24322"} {
		for _, kind := range []model.ForwardKind{model.ForwardLocal, model.ForwardDynamic} {
			tunnel := policySSH("")
			tunnel.Forwards = []model.Forward{{Kind: kind, Bind: bind, Target: "127.0.0.1:5432"}}
			policyError(t, policyCall(t, mcp, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}, nil), api.PolicyDenied)
			policyError(t, policyCall(t, mcp, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel}, nil), api.PolicyDenied)
		}
	}
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := policySSH("")
	tunnel.Name = "Person chosen port"
	tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:24322", Target: "127.0.0.1:5432"}}
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: tunnel}, nil)
}

func TestMCPLiveLeaseAndEphemeralCapIsSharedPerSession(t *testing.T) {
	agent := settings.DefaultAgent()
	agent.AllowTunnels = []string{"t"}
	agent.AllowSSHHosts = []string{"trusted"}
	_, socket, _ := policyServer(t, agent, func(o *engine.Options, _ *model.Config) {
		policyHostConfig(t, o, "Host trusted\n HostName owner.example.invalid\n")
		o.Listening = func(string) bool { return false }
	})
	mcp := leasedDial(t, socket, "mcp", "capacity-agent", "")
	resumed := leasedDial(t, socket, "mcp", "capacity-agent", mcp.Session())
	var first api.Lease
	for i := 0; i < engine.MaxAgentLeases/2; i++ {
		var lease api.Lease
		policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, &lease)
		if i == 0 {
			first = lease
		}
	}
	p := api.RunEphemeralParams{Tunnel: model.Tunnel{Name: "Ephemeral", Type: model.TypeSSH, SSH: &model.SSH{Host: "trusted", Flags: []string{"-N"}}, Forwards: []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:5432"}}}}
	for i := 0; i < engine.MaxAgentLeases/2; i++ {
		policyOK(t, mcp, "tunnel.runEphemeral", p, nil)
	}
	for _, c := range []*client.Client{mcp, resumed} {
		policyError(t, policyCall(t, c, "lease.open", api.OpenLeaseParams{ID: "t"}, nil), api.PolicyDenied)
		policyError(t, policyCall(t, c, "tunnel.runEphemeral", p, nil), api.PolicyDenied)
	}
	var leases []api.Lease
	policyOK(t, mcp, "lease.list", api.Empty{}, &leases)
	if len(leases) != engine.MaxAgentLeases {
		t.Fatalf("cap allowed %d leases", len(leases))
	}
	other := leasedDial(t, socket, "mcp", "capacity-agent", "")
	policyOK(t, other, "lease.open", api.OpenLeaseParams{ID: "t"}, nil)
	policyOK(t, mcp, "lease.close", api.IDParams{ID: first.ID}, nil)
	policyOK(t, resumed, "lease.open", api.OpenLeaseParams{ID: "t"}, nil)
	person := leasedDial(t, socket, "gui", "person", "")
	for i := 0; i <= engine.MaxAgentLeases; i++ {
		policyOK(t, person, "lease.open", api.OpenLeaseParams{ID: "t"}, nil)
	}
	// Reading redacted definitions must not change the stored source objects.
	before := policyTunnel(t, person, "t").Tunnel
	policyTunnel(t, mcp, "t")
	if after := policyTunnel(t, person, "t").Tunnel; !reflect.DeepEqual(before, after) {
		t.Fatal("MCP redaction mutated person definition")
	}
}

func TestMCPPromotionRejectsLegacyPersistenceMetadata(t *testing.T) {
	for _, field := range []string{"startOnLaunch", "profile", "groups", "favorite"} {
		t.Run(field, func(t *testing.T) {
			_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, cfg *model.Config) {
				cfg.Profiles = []model.Profile{{ID: "work", Name: "Work"}}
				cfg.Groups = []model.Group{{ID: "stack", Name: "Stack"}}
				tunnel := policySSH("tmp-legacy")
				switch field {
				case "startOnLaunch":
					tunnel.StartOnLaunch = true
				case "profile":
					tunnel.Profile = "work"
				case "groups":
					tunnel.Groups = []string{"stack"}
				case "favorite":
					tunnel.Favorite = true
				}
				now := o.Clock.Now()
				st := corestate.Runtime{Version: 1, Ephemeral: []model.Tunnel{tunnel}, Leases: []model.Lease{{ID: "ls_legacy", TunnelID: tunnel.ID, Holder: api.Actor{Kind: "mcp", Name: "legacy-agent"}, Session: "legacy-session-token", CreatedAt: now, ExpiresAt: now.Add(time.Hour), StartedIt: true}}, Owners: map[string]corestate.RuntimeOwner{tunnel.ID: {LeaseStarted: true}}}
				if err := (corestate.RuntimeStore{Path: filepath.Join(o.StateDir, "runtime.json")}).Save(st); err != nil {
					t.Fatal(err)
				}
				// A recovered orphan preserves the legacy ephemeral through New;
				// neither identity checks nor stops touch real OS processes.
				if err := (&supervisor.FileRecords{Path: filepath.Join(o.StateDir, "processes.json")}).Add(supervisor.Record{TunnelID: tunnel.ID, PID: 12345, Identity: "legacy-survivor"}); err != nil {
					t.Fatal(err)
				}
				o.ProcessIdentity = func(int) (string, error) { return "legacy-survivor", nil }
				o.StopProcess = func(int, string, time.Duration) error { return nil }
			})
			mcp := leasedDial(t, socket, "mcp", "legacy-agent", "legacy-session-token")
			person := leasedDial(t, socket, "gui", "person", "")
			policyError(t, policyCall(t, mcp, "tunnel.promote", api.PromoteTunnelParams{ID: "tmp-legacy", SavedID: "legacy-saved"}, nil), api.PolicyDenied)
			if got := policyTunnel(t, person, "tmp-legacy"); !got.Owner.Ephemeral {
				t.Fatal("denied promotion changed ephemeral")
			}
			var approvals []api.Approval
			policyOK(t, person, "approval.list", api.Empty{}, &approvals)
			if len(approvals) != 0 {
				t.Fatalf("unsafe legacy promotion offered approval: %+v", approvals)
			}
		})
	}
}

func TestMCPRejectsToolOptionAndHostnameInjection(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "argument-agent", "")
	for _, field := range []string{"ngrok.addr", "ngrok.domain", "cloudflared.name", "cloudflared.url", "cloudflared.hostname", "ssh.hostname"} {
		for _, value := range []string{"-attacker-option", "host with space", "host\nargument"} {
			tunnel := policySSH("")
			switch field {
			case "ngrok.addr", "ngrok.domain":
				tunnel.Type = model.TypeNgrok
				tunnel.SSH = nil
				tunnel.Ngrok = &model.Ngrok{Proto: "http", Addr: "3000"}
				if field == "ngrok.addr" {
					tunnel.Ngrok.Addr = value
				} else {
					tunnel.Ngrok.Domain = value
				}
			case "cloudflared.name", "cloudflared.url", "cloudflared.hostname":
				tunnel.Type = model.TypeCloudflared
				tunnel.SSH = nil
				tunnel.Cloudflared = &model.Cloudflared{URL: "http://localhost:3000"}
				switch field {
				case "cloudflared.name":
					tunnel.Cloudflared.Name = value
				case "cloudflared.url":
					tunnel.Cloudflared.URL = value
				case "cloudflared.hostname":
					tunnel.Cloudflared.Hostname = value
				}
			case "ssh.hostname":
				tunnel.SSH.Hostname = value
			}
			policyError(t, policyCall(t, mcp, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: tunnel}, nil), api.PolicyDenied)
		}
	}
}
