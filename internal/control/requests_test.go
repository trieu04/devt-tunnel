package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

func requestOpen(t *testing.T, c *client.Client, p api.OpenRequestParams) api.OpenRequestResult {
	t.Helper()
	var result api.OpenRequestResult
	policyOK(t, c, "tunnel.openRequest", p, &result)
	return result
}
func requestWait(t *testing.T, c *client.Client, id string) api.OpenRequestResult {
	t.Helper()
	var result api.OpenRequestResult
	policyOK(t, c, "tunnel.waitRequest", api.WaitRequestParams{RequestID: id}, &result)
	return result
}

func TestOpenRequestConcurrentReconnectAndConflicts(t *testing.T) {
	a := settings.DefaultAgent()
	a.AllowTunnels = []string{"t"}
	_, socket, _ := policyServer(t, a, nil)
	first := leasedDial(t, socket, "mcp", "request-agent", "")
	second := leasedDial(t, socket, "mcp", "request-agent", first.Session())
	p := api.OpenRequestParams{RequestID: "one", ID: "t", Purpose: "test reuse", TTL: "2m"}
	const n = 20
	results, errs := make([]api.OpenRequestResult, n), make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := first
			if i%2 != 0 {
				c = second
			}
			errs[i] = c.Call(context.Background(), "tunnel.openRequest", p, &results[i])
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if errs[i] != nil || result.Lease == nil || result.Lease.ID != results[0].Lease.ID || result.TunnelID != "t" || result.Lease.Session != "" {
			t.Fatalf("retry %d = %+v, err=%v", i, result, errs[i])
		}
	}
	var leases []api.Lease
	policyOK(t, first, "lease.list", api.Empty{}, &leases)
	if len(leases) != 1 {
		t.Fatalf("duplicate leases: %+v", leases)
	}
	policyOK(t, first, "tunnel.waitReady", api.IDParams{ID: "t"}, nil)
	if got := requestWait(t, second, p.RequestID); got.Status != "ready" {
		t.Fatalf("ready=%+v", got)
	}
	for _, change := range []func(*api.OpenRequestParams){
		func(p *api.OpenRequestParams) { p.TTL = "3m" },
		func(p *api.OpenRequestParams) { p.Purpose = "changed purpose" },
		func(p *api.OpenRequestParams) { p.ID = "different" },
		func(p *api.OpenRequestParams) { p.ID = ""; tunnel := policySSH(""); p.Tunnel = &tunnel },
	} {
		changed := p
		change(&changed)
		policyError(t, policyCall(t, first, "tunnel.openRequest", changed, nil), api.Conflict)
	}
	first.Close()
	second.Close()
	resumed := leasedDial(t, socket, "mcp", "request-agent", first.Session())
	got := requestOpen(t, resumed, p)
	if got.Lease == nil || got.Lease.ID != results[0].Lease.ID {
		t.Fatalf("reconnect duplicated lease: %+v", got)
	}
	outsider := leasedDial(t, socket, "mcp", "request-agent", "")
	policyError(t, policyCall(t, outsider, "tunnel.waitRequest", api.WaitRequestParams{RequestID: p.RequestID}, nil), api.Unknown)
	// Equal request IDs in distinct sessions are not coupled.
	other := requestOpen(t, outsider, p)
	if other.Lease.ID == got.Lease.ID {
		t.Fatal("request ID leaked across sessions")
	}
}

func TestOpenRequestApprovalWaitUsesOriginalAndFingerprint(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), func(o *engine.Options, _ *model.Config) {
		o.Listening = func(string) bool { return false }
	})
	mcp := leasedDial(t, socket, "mcp", "pending-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	tunnel := policySSH("")
	tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:5432"}}
	p := api.OpenRequestParams{RequestID: "original", Tunnel: &tunnel, Purpose: "run migration", TTL: "2m"}
	pending := requestOpen(t, mcp, p)
	if pending.Status != "pending_approval" || pending.ApprovalID == "" || pending.ExpiresAt == nil || !strings.Contains(pending.Message, "same requestId") {
		t.Fatalf("approval is not a normal human-needed result: %+v", pending)
	}
	before := policyGetApproval(t, person, pending.ApprovalID)
	// Mutating caller-side input cannot change the original retried by wait.
	tunnel.SSH.Hostname = "mutated-client.example.invalid"
	if again := requestWait(t, mcp, p.RequestID); again.ApprovalID != pending.ApprovalID {
		t.Fatalf("poll drifted: %+v", again)
	}
	policyDecide(t, person, pending.ApprovalID, "approve", "once")
	opened := requestWait(t, mcp, p.RequestID)
	if opened.Lease == nil || opened.TunnelID != before.TunnelID {
		t.Fatalf("wait lost prepared args: %+v", opened)
	}
	view := policyTunnel(t, person, opened.TunnelID)
	if view.Tunnel.SSH.Hostname != "bastion.example.invalid" || !strings.Contains(strings.Join(before.Argv, " "), view.Tunnel.Forwards[0].Bind) {
		t.Fatalf("stored original/allocation drifted: %+v / %+v", before, view)
	}
	if again := requestWait(t, mcp, p.RequestID); again.Lease == nil || again.Lease.ID != opened.Lease.ID {
		t.Fatalf("wait duplicated execution: %+v", again)
	}
	var check api.CheckRequestResult
	policyOK(t, mcp, "tunnel.checkRequest", api.OpenRequestParams{ID: opened.TunnelID}, &check)
	if check.Decision != "deny" || check.Preview != "" {
		t.Fatalf("ephemeral saved-ID check=%+v", check)
	}
	// A separate request ID cannot consume the first request's approval/grant.
	tunnel.SSH.Hostname = "bastion.example.invalid"
	p.RequestID = "independent"
	independent := requestOpen(t, mcp, p)
	if independent.Status != "pending_approval" || independent.ApprovalID == pending.ApprovalID {
		t.Fatalf("request approvals coupled: %+v", independent)
	}
	// Saved definitions are rebuilt; a changed command cannot consume approval.
	saved := api.OpenRequestParams{RequestID: "saved-fence", ID: "t", Purpose: "test fence"}
	fenced := requestOpen(t, mcp, saved)
	policyDecide(t, person, fenced.ApprovalID, "approve", "once")
	edited := policyTunnel(t, person, "t").Tunnel
	edited.SSH.Hostname = "changed.example.invalid"
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: edited}, nil)
	fenced = requestWait(t, mcp, saved.RequestID)
	if fenced.Status != "pending_approval" {
		t.Fatalf("changed fingerprint executed: %+v", fenced)
	}
	if a := policyGetApproval(t, person, fenced.ApprovalID); a.Status != "pending" || !strings.Contains(strings.Join(a.Argv, " "), "changed.example.invalid") {
		t.Fatalf("new fingerprint approval=%+v", a)
	}
}

func TestOpenRequestDeniedTimeoutAndRequestCapacity(t *testing.T) {
	s, socket, clock := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "terminal-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	p := api.OpenRequestParams{RequestID: "denied", ID: "t", Purpose: "test denial"}
	pending := requestOpen(t, mcp, p)
	policyDecide(t, person, pending.ApprovalID, "deny", "once")
	if got := requestWait(t, mcp, p.RequestID); got.Status != "denied" || got.Lease != nil {
		t.Fatalf("denial=%+v", got)
	}
	p.RequestID = "timeout"
	requestOpen(t, mcp, p)
	clock.advance(engine.ApprovalTimeout)
	if got := requestWait(t, mcp, p.RequestID); got.Status != "timed_out" || got.Lease != nil {
		t.Fatalf("timeout=%+v", got)
	}
	policyNoLeases(t, person)
	// A fresh session cannot grow the cache forever, even with invalid IDs.
	capped := leasedDial(t, socket, "mcp", "capacity-agent", "")
	for i := range engine.MaxOpenRequestsPerSession {
		p := api.OpenRequestParams{RequestID: fmt.Sprint(i), ID: "absent", Purpose: "test cap"}
		if err := policyCall(t, capped, "tunnel.openRequest", p, nil); err == nil {
			t.Fatal("unknown saved tunnel unexpectedly opened")
		}
	}
	p = api.OpenRequestParams{RequestID: "overflow", ID: "absent", Purpose: "test cap"}
	err := policyError(t, policyCall(t, capped, "tunnel.openRequest", p, nil), api.PolicyDenied)
	if !strings.Contains(err.Message, "cache is full") {
		t.Fatalf("unclear cap error: %v", err)
	}
	clock.advance(engine.OpenRequestRetention)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	policyError(t, policyCall(t, capped, "tunnel.waitRequest", api.WaitRequestParams{RequestID: "0"}, nil), api.Unknown)
	if err := policyCall(t, capped, "tunnel.openRequest", p, nil); err == nil || strings.Contains(err.Error(), "cache is full") {
		t.Fatalf("completed entries not pruned: %v", err)
	}
}

func TestCheckRequestPolicyAliasFactsRedactionAndNoKeychain(t *testing.T) {
	store := &policyCountingStore{}
	agent := settings.DefaultAgent()
	agent.AllowSSHHosts = []string{"trusted", "fake-alias"}
	agent.AllowTunnels = []string{"t", "unsafe"}
	var runtimePath string
	s, socket, _ := policyServer(t, agent, func(o *engine.Options, cfg *model.Config) {
		runtimePath = filepath.Join(o.StateDir, "runtime.json")
		o.Secrets = store
		o.Listening = func(string) bool { return false }
		policyHostConfig(t, o, "Host trusted\n HostName real.example.invalid\n")
		cfg.Tunnels[0].SSH.PassphraseSecret = "saved-secret-reference"
		cfg.Tunnels = append(cfg.Tunnels, model.Tunnel{ID: "unsafe", Name: "Private command", Type: model.TypeCommand, Command: &model.Command{Line: "echo DO-NOT-LEAK-SAVED-COMMAND", ReadyLog: "ready"}})
	})
	mcp := leasedDial(t, socket, "mcp", "check-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	policyOK(t, person, "secret.forget", api.Empty{}, nil)
	reads := store.reads.Load()
	beforeRuntime, _ := os.ReadFile(runtimePath)
	var result api.CheckRequestResult
	for _, id := range []string{"t", "unsafe"} {
		policyOK(t, mcp, "tunnel.checkRequest", api.OpenRequestParams{ID: id}, &result)
		if result.Preview != "" {
			t.Fatalf("saved preview leaked: %+v", result)
		}
		if id == "unsafe" && result.Decision != "ask" {
			t.Fatalf("unsafe saved command was allowed: %+v", result)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "DO-NOT-LEAK") || strings.Contains(string(encoded), "saved-secret-reference") {
			t.Fatalf("saved execution leaked: %s", encoded)
		}
		for _, f := range result.Findings {
			if f.Field == "secret" {
				t.Fatalf("secret presence finding: %+v", f)
			}
		}
	}
	for _, tc := range []struct{ alias, secret, want string }{
		{"trusted", "", "allow"}, {"fake-alias", "", "ask"},
		{"trusted", "existing-name", "ask"}, {"trusted", "missing-name", "ask"},
		{"fake-alias", "missing-name", "deny"},
	} {
		tunnel := policySSH("")
		tunnel.SSH.Hostname, tunnel.SSH.Host, tunnel.SSH.PassphraseSecret = "", tc.alias, tc.secret
		tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, ListenPort: "auto", Target: "127.0.0.1:5432"}}
		policyOK(t, mcp, "tunnel.checkRequest", api.OpenRequestParams{Tunnel: &tunnel}, &result)
		if result.Decision != tc.want {
			t.Fatalf("alias=%q secret=%q: %+v", tc.alias, tc.secret, result)
		}
		if tc.want != "deny" && result.Preview == "" {
			t.Fatalf("inline preview missing: %+v", result)
		}
		for _, f := range result.Findings {
			if f.Field == "secret" {
				t.Fatalf("inline presence finding: %+v", f)
			}
		}
	}
	if got := store.reads.Load(); got != reads {
		t.Fatalf("dry run read keychain %d times", got-reads)
	}
	var approvals []api.Approval
	policyOK(t, mcp, "approval.list", api.Empty{}, &approvals)
	if len(approvals) != 0 {
		t.Fatalf("check enqueued approvals: %+v", approvals)
	}
	policyNoLeases(t, mcp)
	if s.engine.Active() {
		t.Fatal("dry run started a process")
	}
	afterRuntime, _ := os.ReadFile(runtimePath)
	if string(beforeRuntime) != string(afterRuntime) {
		t.Fatal("dry run mutated runtime storage")
	}
}

func TestPolicyGetNarrowAndDisabledMutationReason(t *testing.T) {
	a := settings.DefaultAgent()
	a.Enabled = false
	a.AllowTunnels = []string{"dev-*"}
	_, socket, _ := policyServer(t, a, func(o *engine.Options, _ *model.Config) { o.PortRange = "31000-31001" })
	mcp := leasedDial(t, socket, "mcp", "policy-resource", "")
	var result map[string]any
	policyOK(t, mcp, "policy.get", api.Empty{}, &result)
	keys := map[string]bool{"enabled": true, "public": true, "allowTunnels": true, "allowSSHHosts": true, "portRange": true, "defaultTTL": true, "maxTTL": true}
	if len(result) != len(keys) {
		t.Fatalf("policy fields=%+v", result)
	}
	for key := range result {
		if !keys[key] {
			t.Fatalf("sensitive/unreviewed policy field %q", key)
		}
	}
	if result["enabled"] != false || result["portRange"] != "31000-31001" {
		t.Fatalf("not effective policy: %+v", result)
	}
	policyError(t, policyCall(t, mcp, "tunnel.openRequest", api.OpenRequestParams{RequestID: "disabled", ID: "t", Purpose: "test disabled"}, nil), api.AgentDisabled)
	policyError(t, policyCall(t, mcp, "tunnel.list", api.Empty{}, nil), api.AgentDisabled)
	policyError(t, policyCall(t, mcp, "tunnel.checkRequest", api.OpenRequestParams{ID: "t"}, nil), api.AgentDisabled)
	var status api.DaemonStatus
	policyOK(t, mcp, api.DaemonStatusMethod, api.Empty{}, &status)
	if len(status.Tunnels) != 0 {
		t.Fatalf("disabled status leaked tunnel definitions: %+v", status.Tunnels)
	}
	person := leasedDial(t, socket, "gui", "person", "")
	policyOK(t, person, "tunnel.list", api.Empty{}, nil)
}

func TestOpenRequestStrictDecodeAndUnsafeSmuggling(t *testing.T) {
	s, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "smuggling-agent", "")
	for _, params := range []any{
		map[string]any{"requestId": "unknown", "id": "t", "purpose": "test", "command": "touch nowhere"},
		map[string]any{"requestId": "nested", "purpose": "test", "tunnel": map[string]any{"type": "ssh", "ssh": map[string]any{"hostname": "example.invalid", "unknownExecutionOption": "bad"}}},
	} {
		err := policyError(t, policyCall(t, mcp, "tunnel.openRequest", params, nil), api.Unknown)
		if err.Code != -32602 {
			t.Fatalf("unknown fields accepted: %v", err)
		}
	}
	for i, mutate := range []func(*model.Tunnel){
		func(t *model.Tunnel) { t.SSH.Options = map[string]string{"ProxyCommand": "touch nowhere"} },
		func(t *model.Tunnel) { t.SSH.ExtraArgs = "-oLocalCommand=bad" },
		func(t *model.Tunnel) { t.SSH.Identity = "/private/key" },
		func(t *model.Tunnel) { t.StartOnLaunch = true },
		func(t *model.Tunnel) { t.Profiles = []string{"work"} },
		func(t *model.Tunnel) { t.Favorite = true },
		func(t *model.Tunnel) { t.Name = "bad\nname" },
	} {
		tunnel := policySSH("")
		mutate(&tunnel)
		p := api.OpenRequestParams{RequestID: fmt.Sprint(i), Tunnel: &tunnel, Purpose: "test unsafe"}
		policyError(t, policyCall(t, mcp, "tunnel.openRequest", p, nil), api.PolicyDenied)
		var result api.CheckRequestResult
		policyOK(t, mcp, "tunnel.checkRequest", p, &result)
		if result.Decision != "deny" {
			t.Fatalf("unsafe check allowed: %+v", result)
		}
	}
	var approvals []api.Approval
	policyOK(t, mcp, "approval.list", api.Empty{}, &approvals)
	if len(approvals) != 0 || s.engine.Active() {
		t.Fatal("smuggled execution mutated state")
	}
	policyNoLeases(t, mcp)
}

func TestDisabledAgentStopsExistingEventSubscription(t *testing.T) {
	_, socket, _ := policyServer(t, settings.DefaultAgent(), nil)
	mcp := leasedDial(t, socket, "mcp", "subscribed-agent", "")
	person := leasedDial(t, socket, "gui", "person", "")
	agentEvents, personEvents := make(chan api.Event, 8), make(chan api.Event, 8)
	mcp.Events(func(event api.Event) {
		select {
		case agentEvents <- event:
		default:
		}
	})
	person.Events(func(event api.Event) {
		select {
		case personEvents <- event:
		default:
		}
	})
	policyOK(t, mcp, api.EventsSubscribe, api.Empty{}, nil)
	policyOK(t, person, api.EventsSubscribe, api.Empty{}, nil)
	policy := settings.DefaultAgent()
	policy.Enabled = false
	policyOK(t, person, "settings.setAgent", api.AgentSettingsParams{Agent: policy}, nil)
	policyOK(t, person, "tunnel.setFavorite", api.FavoriteParams{ID: "t", Favorite: true}, nil)
	select {
	case event := <-personEvents:
		if event.Type != api.EventTunnelsChanged {
			t.Fatalf("unexpected person event: %+v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disabling MCP incorrectly suppressed person events")
	}
	select {
	case event := <-agentEvents:
		t.Fatalf("disabled MCP retained live event access: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
}
