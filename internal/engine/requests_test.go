package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestOpenRequestViewsRetentionAndPrivateArgs(t *testing.T) {
	e, _, clock := ownedEngine(t, nil, model.Config{})
	actor, token := session(t, e, "request-owner")
	tunnel := commandTunnel()
	tunnel.ID = ""
	p := api.OpenRequestParams{RequestID: "owned", Tunnel: &tunnel, Purpose: "view polling", TTL: "5m"}
	opened, err := e.OpenRequest(context.Background(), actor, token, p)
	if err != nil || opened.Lease == nil {
		t.Fatalf("open=%+v, %v", opened, err)
	}
	original := cloneRequestTunnel(tunnel)
	tunnel.Command.Line = "MUTATED-CALLER-ARG"
	r := e.openRequests[openRequestKey{actor, token, "owned"}]
	if r.args.Tunnel.Command.Line != original.Command.Line {
		t.Fatal("original args retained caller pointer")
	}
	readyOwned(t, e, opened.TunnelID)
	e.promptMu.Lock()
	e.prompts["request-test"] = &waitingPrompt{view: api.PendingPrompt{ID: "request-test", Prompt: askpass.Prompt{TunnelID: opened.TunnelID, Kind: askpass.KindHostKey, Text: "DO-NOT-RETURN-PROMPT"}}}
	e.promptMu.Unlock()
	got, err := e.WaitRequest(context.Background(), actor, token, api.WaitRequestParams{RequestID: "owned"})
	if err != nil || got.Status != "needs_user" || got.PromptKind != string(askpass.KindHostKey) {
		t.Fatalf("prompt snapshot=%+v %v", got, err)
	}
	// Returned objects must not mutate cache ownership or expiry state.
	got.Lease.ID = "changed-return"
	*got.ExpiresAt = time.Time{}
	e.promptMu.Lock()
	delete(e.prompts, "request-test")
	e.promptMu.Unlock()
	got, err = e.WaitRequest(context.Background(), actor, token, api.WaitRequestParams{RequestID: "owned"})
	if err != nil || got.Status != "ready" || got.Lease.ID != opened.Lease.ID || got.ExpiresAt.IsZero() {
		t.Fatalf("cached result mutated: %+v %v", got, err)
	}
	if err := e.CloseLease(actor, token, opened.Lease.ID); err != nil {
		t.Fatal(err)
	}
	got, err = e.WaitRequest(context.Background(), actor, token, api.WaitRequestParams{RequestID: "owned"})
	if err != nil || got.Status != "expired" {
		t.Fatalf("closed request=%+v %v", got, err)
	}
	clock.Advance(OpenRequestRetention)
	if err := e.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	if len(e.openRequests) != 0 {
		t.Fatalf("stale request retained: %+v", e.openRequests)
	}
}

func TestOpenRequestGlobalCapAndMaintenancePruning(t *testing.T) {
	e, _, clock := ownedEngine(t, nil, model.Config{})
	// Filling via public API also verifies failed operations remain idempotent,
	// count toward the cap, and do not allocate unbounded stale request state.
	for i := 0; i < MaxOpenRequests/MaxOpenRequestsPerSession; i++ {
		actor, token := session(t, e, fmt.Sprint("cap-", i))
		for j := range MaxOpenRequestsPerSession {
			_, err := e.OpenRequest(context.Background(), actor, token, api.OpenRequestParams{RequestID: fmt.Sprint(j), ID: "missing", Purpose: "bounded failures"})
			if err == nil {
				t.Fatal("unknown saved tunnel opened")
			}
		}
	}
	if len(e.openRequests) != MaxOpenRequests {
		t.Fatalf("cache size=%d", len(e.openRequests))
	}
	actor, token := session(t, e, "global-overflow")
	_, err := e.OpenRequest(context.Background(), actor, token, api.OpenRequestParams{RequestID: "overflow", ID: "missing", Purpose: "cap"})
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.PolicyDenied {
		t.Fatalf("global cap not enforced: %v", err)
	}
	clock.Advance(OpenRequestRetention)
	if err := e.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	if len(e.openRequests) != 0 {
		t.Fatalf("maintenance retained %d stale requests", len(e.openRequests))
	}
}
