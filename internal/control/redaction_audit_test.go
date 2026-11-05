package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

type outputRunner struct{ output chan io.Writer }

func (r outputRunner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	r.output <- req.Output
	_, _ = io.WriteString(req.Output, "ready\n")
	return &leaseProcess{done: make(chan struct{})}, nil
}

func logTestServer(t *testing.T) (*Server, string, *leaseClock, <-chan io.Writer) {
	t.Helper()
	clock := &leaseClock{now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	output := make(chan io.Writer, 1)
	s, socket := testServerWithOptions(t, func(opts *engine.Options) {
		opts.Clock = clock
		opts.Runner = outputRunner{output: output}
		store := &secret.Memory{}
		if err := store.Set("token", "private-secret-value"); err != nil {
			t.Fatal(err)
		}
		// An unrelated vault entry must not be fetched or used by the redactor.
		if err := store.Set("other", "unrelated-secret-value"); err != nil {
			t.Fatal(err)
		}
		opts.Secrets = store
		if err := config.Save(opts.ConfigPath, model.Config{Tunnels: []model.Tunnel{{ID: "t", Name: "Audit test", Type: model.TypeCommand, Command: &model.Command{Line: "echo audit-argv-sentinel", ReadyLog: "ready", SecretEnvs: map[string]string{"TOKEN": "token"}}}}}); err != nil {
			t.Fatal(err)
		}
	})
	return s, socket, clock, output
}

func TestMCPLogsRedactionRawGUIAndEmittedCursor(t *testing.T) {
	_, socket, clock, outputs := logTestServer(t)
	person := leasedDial(t, socket, "gui", "person", "")
	mcp := leasedDial(t, socket, "mcp", "adapter", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := person.Call(ctx, "tunnel.start", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := person.Call(ctx, "tunnel.waitReady", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	var output io.Writer
	select {
	case output = <-outputs:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, _ = io.WriteString(output, "private-")
	_, _ = io.WriteString(output, "secret-value\nunrelated-secret-value\n")
	var raw []string
	if err := person.Call(ctx, "logs.read", api.IDParams{ID: "t"}, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(raw, "\n"), "private-secret-value") {
		t.Fatalf("GUI did not receive raw output: %q", raw)
	}
	var redacted []string
	if err := mcp.Call(ctx, "logs.read", api.IDParams{ID: "t"}, &redacted); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(redacted, "\n")
	if strings.Contains(text, "private-secret-value") || !strings.Contains(text, "[secret:token]") || !strings.Contains(text, "unrelated-secret-value") {
		t.Fatalf("redacted=%q", text)
	}
	var before api.LogsResult
	if err := mcp.Call(ctx, "logs.since", api.LogsParams{ID: "t"}, &before); err != nil {
		t.Fatal(err)
	}
	// Split a secret over separate complete source records and separate polls.
	_, _ = io.WriteString(output, "private-\n")
	var held api.LogsResult
	if err := mcp.Call(ctx, "logs.since", api.LogsParams{ID: "t", Sequence: before.Sequence}, &held); err != nil {
		t.Fatal(err)
	}
	if held.Sequence != before.Sequence || len(held.Lines) != 0 {
		t.Fatalf("advertised buffered record: %+v before=%+v", held, before)
	}
	_, _ = io.WriteString(output, "secret-value\n")
	var completed api.LogsResult
	if err := mcp.Call(ctx, "logs.since", api.LogsParams{ID: "t", Sequence: held.Sequence}, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Sequence != before.Sequence+2 || len(completed.Lines) != 2 || completed.Lines[0].Line != "[secret:token]" || completed.Lines[1].Line != "" {
		t.Fatalf("split redaction=%+v", completed)
	}
	// Idle flush masks a dangling prefix; the raw high-water mark isn't exposed
	// until the conservative mask is emitted.
	_, _ = io.WriteString(output, "private-\n")
	var idle api.LogsResult
	if err := mcp.Call(ctx, "logs.since", api.LogsParams{ID: "t", Sequence: completed.Sequence}, &idle); err != nil {
		t.Fatal(err)
	}
	if idle.Sequence != completed.Sequence {
		t.Fatal("prefix advanced cursor")
	}
	clock.advance(time.Second)
	if err := mcp.Call(ctx, "logs.since", api.LogsParams{ID: "t", Sequence: completed.Sequence}, &idle); err != nil {
		t.Fatal(err)
	}
	if len(idle.Lines) != 1 || idle.Lines[0].Line != "[secret:token]" {
		t.Fatalf("idle=%+v", idle)
	}
}

func TestDaemonAuditApprovalOpenCloseContainsNoSensitivePayload(t *testing.T) {
	_, socket, _, outputs := logTestServer(t)
	person := leasedDial(t, socket, "cli", "person", "")
	mcp := leasedDial(t, socket, "mcp", "adapter", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := api.OpenLeaseParams{ID: "t", Purpose: "private-secret-value purpose-not-in-audit"}
	err := mcp.Call(ctx, "lease.open", p, nil)
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.PendingApproval || rpc.Data.ApprovalID == "" {
		t.Fatalf("ask=%v", err)
	}
	var approval api.Approval
	if err := person.Call(ctx, "approval.get", api.IDParams{ID: rpc.Data.ApprovalID}, &approval); err != nil {
		t.Fatal(err)
	}
	if err := person.Call(ctx, "approval.decide", api.DecideApprovalParams{ID: approval.ID, Decision: "approve", Scope: "once", Code: approval.Code}, nil); err != nil {
		t.Fatal(err)
	}
	var lease api.Lease
	if err := mcp.Call(ctx, "lease.open", p, &lease); err != nil {
		t.Fatal(err)
	}
	var output io.Writer
	select {
	case output = <-outputs:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, _ = io.WriteString(output, "private-secret-value log-text-sentinel\n")
	if err := mcp.Call(ctx, "lease.close", api.IDParams{ID: lease.ID}, nil); err != nil {
		t.Fatal(err)
	}
	var batch api.AuditReadResult
	if err := person.Call(ctx, "audit.read", api.AuditReadParams{}, &batch); err != nil {
		t.Fatal(err)
	}
	want := []audit.Action{audit.ApprovalRequest, audit.ApprovalDecide, audit.LeaseOpen, audit.LeaseClose}
	if len(batch.Events) != len(want) {
		t.Fatalf("events=%+v", batch.Events)
	}
	for i, e := range batch.Events {
		if e.Action != want[i] {
			t.Fatalf("event %d=%s want=%s", i, e.Action, want[i])
		}
	}
	var page api.AuditReadResult
	if err := person.Call(ctx, "audit.read", api.AuditReadParams{Limit: 2}, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Next != batch.Events[1].Sequence {
		t.Fatalf("audit first page=%+v", page)
	}
	if err := person.Call(ctx, "audit.read", api.AuditReadParams{After: page.Next, Limit: 2}, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Action != audit.LeaseOpen || page.Next != batch.Next {
		t.Fatalf("audit next page skipped events: %+v", page)
	}
	encoded, _ := json.Marshal(batch)
	for _, sensitive := range []string{"private-secret-value", "purpose-not-in-audit", "audit-argv-sentinel", "log-text-sentinel", "argv", "summary", "purpose"} {
		if strings.Contains(string(encoded), sensitive) {
			t.Fatalf("audit leaked %q: %s", sensitive, encoded)
		}
	}
	if err := mcp.Call(ctx, "audit.read", api.AuditReadParams{}, nil); !errors.As(err, &rpc) || rpc.Data.Kind != api.PolicyDenied {
		t.Fatalf("MCP audit read=%v", err)
	}
	var filtered api.AuditReadResult
	if err := person.Call(ctx, "audit.read", api.AuditReadParams{Actor: "adapter", Tunnel: "t"}, &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 3 {
		t.Fatalf("filters=%+v", filtered)
	}
	if err := person.Call(ctx, "audit.read", api.AuditReadParams{After: batch.Next}, &filtered); err != nil {
		t.Fatal(err)
	}
	// The refused audit.read itself is a policy denial, never a payload record.
	if len(filtered.Events) != 1 || filtered.Events[0].Action != audit.PolicyDeny {
		t.Fatalf("cursor=%+v", filtered)
	}
}

func TestMCPRedactionSurvivesDefinitionChangesRotationAndReconnect(t *testing.T) {
	_, socket, _, outputs := logTestServer(t)
	person := leasedDial(t, socket, "gui", "person", "")
	first := leasedDial(t, socket, "mcp", "adapter", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	policyOK(t, person, "tunnel.start", api.IDParams{ID: "t"}, nil)
	policyOK(t, person, "tunnel.waitReady", api.IDParams{ID: "t"}, nil)
	var output io.Writer
	select {
	case output = <-outputs:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, _ = io.WriteString(output, "private-secret-value\n")
	check := func(caller interface {
		Call(context.Context, string, any, any) error
	}, want []string) {
		t.Helper()
		var lines []string
		if err := caller.Call(ctx, "logs.read", api.IDParams{ID: "t"}, &lines); err != nil {
			t.Fatal(err)
		}
		text := strings.Join(lines, "\n")
		for _, value := range []string{"private-secret-value", "new-rotated-value", "replacement-secret-value"} {
			if strings.Contains(text, value) {
				t.Fatalf("leaked historical value: %q", text)
			}
		}
		for _, marker := range want {
			if !strings.Contains(text, marker) {
				t.Fatalf("missing %s in %q", marker, text)
			}
		}
	}
	check(first, []string{"[secret:token]"})
	// The still-running process retains its original environment, and the raw
	// ring retains prior output. Removing its latest saved reference cannot make
	// either readable to an existing or a newly connected MCP client.
	view := policyTunnel(t, person, "t")
	view.Tunnel.Command.SecretEnvs = nil
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: view.Tunnel}, nil)
	policyOK(t, person, "secret.delete", api.SecretNameParams{Name: "token"}, nil)
	_, _ = io.WriteString(output, "private-secret-value\n")
	check(first, []string{"[secret:token]"})
	second := leasedDial(t, socket, "mcp", "new-adapter", "")
	check(second, []string{"[secret:token]"})
	policyOK(t, person, "secret.set", api.SetSecretParams{Name: "replacement", Value: "replacement-secret-value", Kind: secret.KindEnv}, nil)
	view.Tunnel.Command.SecretEnvs = map[string]string{"TOKEN": "replacement"}
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: view.Tunnel}, nil)
	_, _ = io.WriteString(output, "private-secret-value replacement-secret-value\n")
	check(second, []string{"[secret:token]", "[secret:replacement]"})
	policyOK(t, person, "secret.set", api.SetSecretParams{Name: "replacement", Value: "new-rotated-value", Kind: secret.KindEnv}, nil)
	_, _ = io.WriteString(output, "replacement-secret-value new-rotated-value\n")
	check(first, []string{"[secret:token]", "[secret:replacement]"})
	third := leasedDial(t, socket, "mcp", "reconnected-adapter", "")
	check(third, []string{"[secret:token]", "[secret:replacement]"})
	// A current named secret that cannot be fetched never causes a raw fallback.
	policyOK(t, person, "secret.delete", api.SecretNameParams{Name: "replacement"}, nil)
	var rpc *api.Error
	if err := third.Call(ctx, "logs.read", api.IDParams{ID: "t"}, nil); !errors.As(err, &rpc) || rpc.Data.Kind != api.NeedsSecret {
		t.Fatalf("missing current secret read=%v", err)
	}
}

func TestMCPRedactionNewRingWithReusedTunnelID(t *testing.T) {
	_, socket, _, outputs := logTestServer(t)
	person := leasedDial(t, socket, "gui", "person", "")
	mcp := leasedDial(t, socket, "mcp", "adapter", "")
	policyOK(t, person, "tunnel.start", api.IDParams{ID: "t"}, nil)
	policyOK(t, person, "tunnel.waitReady", api.IDParams{ID: "t"}, nil)
	first := <-outputs
	_, _ = io.WriteString(first, "private-secret-value\n")
	var old api.LogsResult
	policyOK(t, mcp, "logs.since", api.LogsParams{ID: "t"}, &old)
	if !strings.Contains(old.Lines[len(old.Lines)-1].Line, "[secret:token]") {
		t.Fatalf("old redaction=%+v", old)
	}
	policyOK(t, person, "tunnel.delete", api.IDParams{ID: "t"}, nil)
	policyOK(t, person, "secret.set", api.SetSecretParams{Name: "replacement", Value: "replacement-secret-value", Kind: secret.KindEnv}, nil)
	definition := model.Tunnel{Name: "t", Type: model.TypeCommand, Command: &model.Command{Line: "echo ready", ReadyLog: "ready", SecretEnvs: map[string]string{"TOKEN": "replacement"}}}
	var id string
	policyOK(t, person, "tunnel.save", api.SaveTunnelParams{Tunnel: definition}, &id)
	if id != "t" {
		t.Fatalf("did not recreate same ID: %q", id)
	}
	policyOK(t, person, "tunnel.start", api.IDParams{ID: id}, nil)
	policyOK(t, person, "tunnel.waitReady", api.IDParams{ID: id}, nil)
	second := <-outputs
	for i := uint64(0); i < old.Sequence+2; i++ {
		_, _ = io.WriteString(second, "padding\n")
	}
	_, _ = io.WriteString(second, "replacement-secret-value\n")
	var current api.LogsResult
	policyOK(t, mcp, "logs.since", api.LogsParams{ID: id}, &current)
	text := ""
	for _, line := range current.Lines {
		text += line.Line + "\n"
	}
	if strings.Contains(text, "replacement-secret-value") || !strings.Contains(text, "[secret:replacement]") {
		t.Fatalf("reused ring retained wrong matcher: %q", text)
	}
}

func TestAuditFailureRefusesNewSpawnsButNeverPreventsCleanup(t *testing.T) {
	for _, operation := range []string{"close", "expire", "person stop"} {
		t.Run(operation, func(t *testing.T) {
			agent := settings.DefaultAgent()
			agent.AllowTunnels = []string{"t"}
			var trail *audit.Writer
			s, socket, clock := policyServer(t, agent, func(o *engine.Options, _ *model.Config) {
				var err error
				trail, err = audit.Open(o.StateDir, audit.Options{})
				if err != nil {
					t.Fatal(err)
				}
				o.Audit = trail
				t.Cleanup(func() { trail.Close() })
			})
			mcp := leasedDial(t, socket, "mcp", "adapter", "")
			person := leasedDial(t, socket, "gui", "person", "")
			var lease api.Lease
			policyOK(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t", TTL: "1m"}, &lease)
			policyOK(t, mcp, "tunnel.waitReady", api.IDParams{ID: "t"}, nil)
			if err := trail.Close(); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "close":
				err = policyCall(t, mcp, "lease.close", api.IDParams{ID: lease.ID}, nil)
			case "expire":
				clock.advance(time.Minute)
				err = s.engine.ExpireLeases()
			case "person stop":
				err = policyCall(t, person, "tunnel.stop", api.IDParams{ID: "t"}, nil)
			}
			if err == nil {
				t.Fatal("closed audit writer error was hidden")
			}
			policyStopped(t, person, "t")
			// A subsequent allowed policy decision still cannot spawn without audit.
			if err := policyCall(t, mcp, "lease.open", api.OpenLeaseParams{ID: "t"}, nil); err == nil {
				t.Fatal("spawn admitted with closed audit writer")
			}
			policyStopped(t, person, "t")
		})
	}
}
