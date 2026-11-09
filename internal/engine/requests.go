package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/trieu04/devt-tunnel/internal/api"
)

const MaxOpenRequestsPerSession = 64
const MaxOpenRequests = 1024
const OpenRequestRetention = time.Minute

// Memory-only, like approvals. Reconnects with the same daemon session retain
// their request results; a daemon restart requires a fresh request/approval.
type openRequestKey struct {
	actor       api.Actor
	session, id string
}
type openRequest struct {
	args        api.OpenRequestParams // private deep copy of the ORIGINAL, unallocated args
	fingerprint string
	result      api.OpenRequestResult
	err         error
	terminalAt  time.Time
}

type openRequestContextKey struct{}

// Keep approvals for separate request IDs independent, while preserving the
// existing fingerprint/actor/session/operation checks in ordinary admission.
func approvalOperationKey(ctx context.Context, original string) string {
	if id, ok := ctx.Value(openRequestContextKey{}).(string); ok {
		return requestKey(struct{ Original, RequestID string }{original, id})
	}
	return original
}

func validateOpenRequest(p api.OpenRequestParams, opening bool) error {
	if (p.ID == "") == (p.Tunnel == nil) {
		return api.NewError(api.InvalidSpec, "provide exactly one saved id or tunnel")
	}
	if (opening && p.RequestID == "") || len(p.RequestID) > 256 || !utf8.ValidString(p.RequestID) || strings.ContainsFunc(p.RequestID, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return api.NewError(api.InvalidSpec, "requestId must be nonempty printable UTF-8 and at most 256 bytes")
	}
	if opening && strings.TrimSpace(p.Purpose) == "" {
		return api.NewError(api.InvalidSpec, "purpose is required")
	}
	return invalidSpec(validPurpose(p.Purpose))
}

func (s *Engine) OpenRequest(ctx context.Context, actor api.Actor, session string, p api.OpenRequestParams) (api.OpenRequestResult, error) {
	if err := validateOpenRequest(p, true); err != nil {
		return api.OpenRequestResult{}, err
	}
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if err := s.requestSession(ctx, actor, session); err != nil {
		return api.OpenRequestResult{}, err
	}
	s.pruneOpenRequestsLocked()
	key := openRequestKey{actor, session, p.RequestID}
	fingerprint := requestKey(p)
	if r := s.openRequests[key]; r != nil {
		if r.fingerprint != fingerprint {
			return api.OpenRequestResult{}, api.NewError(api.Conflict, "requestId was already used with different open arguments")
		}
		return s.pollOpenRequestLocked(ctx, actor, session, r)
	}
	count := 0
	for k := range s.openRequests {
		if k.session == session {
			count++
		}
	}
	if count >= MaxOpenRequestsPerSession || len(s.openRequests) >= MaxOpenRequests {
		return api.OpenRequestResult{}, api.NewError(api.PolicyDenied, "open request cache is full; wait for completed requests to expire")
	}
	if s.openRequests == nil {
		s.openRequests = make(map[openRequestKey]*openRequest)
	}
	// JSON and the public API cannot retain pointers into this engine-owned copy.
	if p.Tunnel != nil {
		t := cloneRequestTunnel(*p.Tunnel)
		p.Tunnel = &t
	}
	r := &openRequest{args: p, fingerprint: fingerprint, result: api.OpenRequestResult{RequestID: p.RequestID, Status: "starting", Endpoints: []api.Endpoint{}}}
	s.openRequests[key] = r
	return s.executeOpenRequestLocked(ctx, actor, session, r)
}

func (s *Engine) WaitRequest(ctx context.Context, actor api.Actor, session string, p api.WaitRequestParams) (api.OpenRequestResult, error) {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if err := s.requestSession(ctx, actor, session); err != nil {
		return api.OpenRequestResult{}, err
	}
	s.pruneOpenRequestsLocked()
	r := s.openRequests[openRequestKey{actor, session, p.RequestID}]
	if r == nil {
		return api.OpenRequestResult{}, api.NewError(api.Unknown, "open request not found in this session")
	}
	return s.pollOpenRequestLocked(ctx, actor, session, r)
}

func (s *Engine) requestSession(ctx context.Context, actor api.Actor, session string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.validSession(actor, session)
}

func (s *Engine) executeOpenRequestLocked(ctx context.Context, actor api.Actor, session string, r *openRequest) (api.OpenRequestResult, error) {
	ctx = context.WithValue(ctx, openRequestContextKey{}, r.args.RequestID)
	var lease api.Lease
	var err error
	if r.args.Tunnel != nil {
		var result api.RunEphemeralResult
		result, err = s.RunEphemeralContext(ctx, actor, session, api.RunEphemeralParams{Tunnel: *r.args.Tunnel, Purpose: r.args.Purpose, TTL: r.args.TTL})
		lease = result.Lease
	} else {
		lease, err = s.OpenLeaseContext(ctx, actor, session, api.OpenLeaseParams{ID: r.args.ID, Purpose: r.args.Purpose, TTL: r.args.TTL})
	}
	if err != nil {
		var rpc *api.Error
		if errors.As(err, &rpc) && rpc.Data.Kind == api.PendingApproval {
			r.result.Status = "pending_approval"
			r.result.ApprovalID = rpc.Data.ApprovalID
			r.result.Message = "A person must approve this in devt-tunnel. Tell them, then call tunnel.waitRequest or retry tunnel.openRequest with the same requestId."
			s.snapshotOpenRequestLocked(actor, session, r)
			return cloneOpenRequestResult(r.result), nil
		}
		r.err = err
		r.result.Status = "failed"
		r.terminalAt = s.clock.Now()
		return api.OpenRequestResult{}, err
	}
	r.result.Lease = &lease
	r.result.TunnelID = lease.TunnelID
	r.result.Summary, r.result.Message = "", ""
	r.result.Status = "starting"
	r.result.ExpiresAt = &lease.ExpiresAt
	s.snapshotOpenRequestLocked(actor, session, r)
	return cloneOpenRequestResult(r.result), nil
}

func (s *Engine) pollOpenRequestLocked(ctx context.Context, actor api.Actor, session string, r *openRequest) (api.OpenRequestResult, error) {
	if r.err != nil {
		return api.OpenRequestResult{}, r.err
	}
	approved := s.snapshotOpenRequestLocked(actor, session, r)
	if approved {
		return s.executeOpenRequestLocked(ctx, actor, session, r)
	}
	return cloneOpenRequestResult(r.result), nil
}

// Returns whether a stored original may be retried, NEVER executes it here.
// Maintenance also calls this so stale entries become terminal and are bounded.
func (s *Engine) snapshotOpenRequestLocked(actor api.Actor, session string, r *openRequest) bool {
	if !r.terminalAt.IsZero() {
		return false
	}
	terminal := func(status, message string) {
		r.result.Status, r.result.Message = status, message
		r.result.Endpoints = []api.Endpoint{}
		r.result.PromptKind = ""
		r.terminalAt = s.clock.Now()
	}
	if r.result.Status == "pending_approval" {
		a, err := s.GetApproval(actor, session, r.result.ApprovalID)
		if err != nil {
			// An expired/pruned approval is never permission to enqueue a new one.
			terminal("timed_out", "Approval is no longer available; this request will not execute.")
			return false
		}
		r.result.Summary = a.Summary
		r.result.ExpiresAt = &a.ExpiresAt
		switch a.Status {
		case "approved":
			return true
		case "denied":
			terminal("denied", "A person denied this request. Do not retry it in another form.")
		case "timed_out":
			terminal("timed_out", "Approval timed out; no tunnel was opened.")
		case "consumed":
			terminal("failed", "Approval was consumed; this request will not execute again.")
		}
		return false
	}
	if r.result.Lease == nil {
		return false
	}
	found := false
	for _, lease := range s.ListLeases(actor, session, true) {
		if lease.ID != r.result.Lease.ID {
			continue
		}
		found = true
		l := lease
		r.result.Lease, r.result.TunnelID, r.result.ExpiresAt = &l, l.TunnelID, &l.ExpiresAt
		if l.Revoked {
			terminal("failed", "Lease was revoked by a person; do not reopen without their consent.")
		} else if !leaseAlive(l, s.clock.Now()) {
			terminal("expired", "Lease expired.")
		}
		break
	}
	if !found {
		terminal("expired", "Lease ended.")
	}
	if !r.terminalAt.IsZero() {
		return false
	}
	r.result.Status, r.result.Message, r.result.PromptKind = "starting", "", ""
	for _, view := range s.TunnelViewsFor(actor) {
		if view.Tunnel.ID != r.result.TunnelID {
			continue
		}
		r.result.Endpoints = append([]api.Endpoint{}, view.Endpoints...)
		switch string(view.Status.State) {
		case "running":
			r.result.Status = "ready"
		case "starting", "reconnecting":
		case "needs_token", "keyring_unavailable":
			r.result.Status, r.result.Message = "needs_user", "A person must provide the required secret or unlock the keychain in devt-tunnel."
		default:
			terminal("failed", "Tunnel did not become ready; inspect redacted logs.")
		}
		break
	}
	// Read only the existing prompt view; never return the process's prompt text.
	s.promptMu.Lock()
	for _, prompt := range s.prompts {
		if prompt.view.Prompt.TunnelID == r.result.TunnelID {
			r.result.Status = "needs_user"
			r.result.PromptKind = string(prompt.view.Prompt.Kind)
			r.result.Message = "SSH is waiting for a person to answer a prompt in devt-tunnel or devt-tunnel prompts. Agents cannot answer it."
			break
		}
	}
	s.promptMu.Unlock()
	return false
}

func cloneOpenRequestResult(r api.OpenRequestResult) api.OpenRequestResult {
	r.Endpoints = append([]api.Endpoint{}, r.Endpoints...)
	if r.Lease != nil {
		l := *r.Lease
		// Sessions are reconnect credentials, not needed in tool results.
		l.Session = ""
		if l.GraceUntil != nil {
			deadline := *l.GraceUntil
			l.GraceUntil = &deadline
		}
		r.Lease = &l
	}
	if r.ExpiresAt != nil {
		expiry := *r.ExpiresAt
		r.ExpiresAt = &expiry
	}
	return r
}

func (s *Engine) pruneOpenRequestsLocked() {
	for key, r := range s.openRequests {
		s.snapshotOpenRequestLocked(key.actor, key.session, r)
		if !r.terminalAt.IsZero() && !s.clock.Now().Before(r.terminalAt.Add(OpenRequestRetention)) {
			// Even a failed tunnel can retain a live claim. Do not forget its
			// request ID and accidentally open a second lease on retry.
			live := false
			if r.result.Lease != nil {
				for _, lease := range s.ListLeases(key.actor, key.session, true) {
					if lease.ID == r.result.Lease.ID && leaseAlive(lease, s.clock.Now()) {
						live = true
						break
					}
				}
			}
			if !live {
				delete(s.openRequests, key)
			}
		}
	}
}

// Must run outside configMu; request polling invokes ordinary admission and
// inspection paths which take that lock. No lock order runs config -> request.
func (s *Engine) pruneOpenRequests() {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	s.pruneOpenRequestsLocked()
}
