package engine

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/policy"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	corestate "github.com/trieu04/devt-tunnel/internal/core/state"
)

const ApprovalTimeout = 5 * time.Minute
const MaxApprovalPurpose = 1024 // UTF-8 bytes, not a container for secrets.
const maxApprovals = 1024
const MaxPendingApprovalsPerSession = 16
const ApprovalRetention = time.Minute

// All queue access is serialized with configMu, including authorization and
// consumption. A grant never schedules execution: the owning agent must retry.
type approvalRequest struct {
	terminalAt time.Time
	view       api.Approval
	key        string
	action     policy.Action
	tunnel     model.Tunnel // Prepared auto-port definition; never a client authority.
}

func requestKey(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Engine) agentSettings() settings.Settings {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	cfg := s.settings
	a := cfg.AgentPolicy()
	a.AllowTunnels = slices.Clone(a.AllowTunnels)
	a.AllowSSHHosts = slices.Clone(a.AllowSSHHosts)
	cfg.Agent = &a
	return cfg
}

func (s *Engine) mutationEnabled(actor api.Actor, session string) error {
	if actor.Kind == "mcp" && !s.agentSettings().AgentPolicy().Enabled {
		return s.denySession(actor, session, "", "")
	}
	return nil
}

// Denied methods have no request fields copied to audit metadata.
func (s *Engine) PolicyRefusal(actor api.Actor) error { return s.policyDenied(actor, "") }
func (s *Engine) policyDenied(actor api.Actor, id string) error {
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.PolicyDeny, TunnelID: id, Decision: audit.Deny, Result: audit.ResultDenied}); err != nil {
		return err
	}
	return api.NewError(api.PolicyDenied, "agent policy denied this operation")
}

func auditActor(actor api.Actor) audit.Actor {
	return audit.Actor{Kind: audit.ActorKind(actor.Kind), Name: actor.Name}
}
func (s *Engine) recordAudit(event audit.Event) error {
	if s.audit == nil {
		return nil
	} // Standalone embedded engines may opt out.
	_, err := s.audit.Append(event)
	return err
}
func (s *Engine) ReadAudit(p api.AuditReadParams) (api.AuditReadResult, error) {
	limit := p.Limit
	if limit == 0 {
		limit = 200
	}
	if limit < 1 || limit > 1000 {
		return api.AuditReadResult{}, api.NewError(api.Unknown, "audit limit must be between 1 and 1000")
	}
	batch, err := s.auditReader.Read(p.After, audit.Filter{Actor: p.Actor, Tunnel: p.Tunnel, Since: p.Since})
	if err != nil {
		return api.AuditReadResult{}, err
	}
	// Retained files may exceed the RPC scanner's message limit. Bound each
	// response by count AND bytes, leaving generous room for the envelope.
	size, count := 0, 0
	for _, event := range batch.Events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return api.AuditReadResult{}, err
		}
		if count == limit || size+len(encoded) > api.MaxMessageSize/2 {
			break
		}
		size += len(encoded) + 1
		count++
	}
	if count == 0 && len(batch.Events) > 0 {
		return api.AuditReadResult{}, api.NewError(api.Internal, "audit event exceeds response limit")
	}
	if count < len(batch.Events) {
		batch.Next = batch.Events[count-1].Sequence
		batch.Events = batch.Events[:count]
	}
	if batch.Events == nil {
		batch.Events = []audit.Event{}
	}
	return api.AuditReadResult{Events: batch.Events, Next: batch.Next, Oldest: batch.Oldest, Lost: batch.Lost}, nil
}

func validPurpose(purpose string) error {
	if len(purpose) > MaxApprovalPurpose || !utf8.ValidString(purpose) {
		return api.NewError(api.Unknown, "purpose must be valid UTF-8 and at most 1024 bytes")
	}
	return nil
}

// configMu must be held. Returns the freshly built fingerprint even for an
// allow-list grant, so the supervisor cannot rebuild a different approved spec.
func (s *Engine) authorize(actor api.Actor, session string, action policy.Action, t model.Tunnel, purpose, key string) (string, error) {
	if actor.Kind != "mcp" {
		return "", nil
	}
	if err := validPurpose(purpose); err != nil {
		return "", err
	}
	if action != policy.ActionOpenSaved && action != policy.ActionStartSaved {
		if err := s.agentBuilt(actor, session, t); err != nil {
			return "", err
		}
	}
	cfg := s.agentSettings()
	decision, reason := policy.Decide(actor, action, t, cfg)
	if decision == policy.Deny {
		return "", s.denySession(actor, session, t.ID, "")
	}
	spec, err := s.manager.BuildDraft(t)
	if err != nil {
		return "", err
	}
	aliasKnown := true
	if t.SSH != nil && t.SSH.Host != "" {
		// Check the exact SSH config selected by this freshly built command, not
		// merely t.Profiles[0] (the active profile may select another config).
		aliasKnown = s.builtAliasExists(t.SSH.Host, spec)
		if !aliasKnown {
			if t.SSH.PassphraseSecret != "" && action != policy.ActionOpenSaved && action != policy.ActionStartSaved {
				return "", s.denySession(actor, session, t.ID, "SSH passphrase secrets require a literal config alias")
			}
			cfg.Agent.AllowSSHHosts = nil
			decision, reason = policy.Decide(actor, action, t, cfg)
		}
	}
	fingerprint := spec.Fingerprint()
	if err := s.expireApprovalsLocked(); err != nil {
		return "", err
	}
	for i := len(s.approvalOrder) - 1; i >= 0; i-- {
		r := s.approvals[s.approvalOrder[i]]
		if r.view.Actor != actor || r.view.Session != session || r.key != key || r.action != action {
			continue
		}
		if r.view.Status == "pending" || r.view.Status == "approved" {
			decision = policy.Ask
		}
		if r.view.Fingerprint != fingerprint {
			continue
		}
		switch r.view.Status {
		case "pending":
			return "", pendingApproval(r.view.ID)
		case "denied", "timed_out":
			return "", s.denySession(actor, session, t.ID, "")
		case "approved":
			if r.view.Scope != "session" {
				r.view.Status = "consumed"
				r.terminalAt = s.clock.Now()
			}
			return fingerprint, nil
		}
	}
	if decision == policy.Allow {
		return fingerprint, nil
	}
	// Bound memory and UI spam without dropping live grants/requests. Completed
	// requests expire after a short retention even for connected sessions.
	s.pruneApprovalsLocked()
	pending := 0
	for _, r := range s.approvals {
		if r.view.Session == session && r.view.Status == "pending" {
			pending++
		}
	}
	if pending >= MaxPendingApprovalsPerSession {
		return "", s.denySession(actor, session, t.ID, "agent session has reached its limit of 16 pending approvals")
	}
	if len(s.approvals) >= maxApprovals {
		return "", api.NewError(api.PolicyDenied, "approval queue is full")
	}
	choices := []string{"once", "session"}
	if aliasKnown && policy.CanAlwaysAllow(actor, action, t, cfg) {
		choices = append(choices, "always")
	}
	now := s.clock.Now()
	snapshot := corestate.Runtime{Ephemeral: []model.Tunnel{t}}.Clone().Ephemeral[0]
	r := &approvalRequest{key: key, action: action, tunnel: snapshot, view: api.Approval{
		ID: randomID("ap_"), Actor: actor, Session: session, Purpose: purpose,
		RequestedName: t.Name, Summary: approvalSummary(action, t, reason), TunnelID: t.ID, Action: string(action),
		Argv: append([]string{spec.Tool}, spec.Args...), Fingerprint: fingerprint,
		Choices: choices, Code: strings.ToUpper(randomID("")[:8]), Status: "pending", CreatedAt: now, ExpiresAt: now.Add(ApprovalTimeout),
	}}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.ApprovalRequest, TunnelID: t.ID, ApprovalID: r.view.ID, Fingerprint: fingerprint, Decision: audit.Ask, Result: audit.ResultPendingApproval}); err != nil {
		return "", err
	}
	s.approvals[r.view.ID] = r
	s.approvalOrder = append(s.approvalOrder, r.view.ID)
	// Subscriber callbacks may query the queue; never synchronously invoke them
	// from inside the configuration transaction.
	go s.emit(api.Event{Type: api.EventApprovalAdded, ApprovalID: r.view.ID})
	return "", pendingApproval(r.view.ID)
}

func pendingApproval(id string) error {
	err := api.NewError(api.PendingApproval, "a person must approve this request; poll approval.get and retry")
	err.Data.ApprovalID = id
	return err
}

// Prepared requests keep auto allocation stable across an approval/retry. The
// incoming, unallocated definition is hashed, never trusted by ID alone.
func (s *Engine) preparedEphemeral(actor api.Actor, session, key string) (model.Tunnel, bool) {
	for i := len(s.approvalOrder) - 1; i >= 0; i-- {
		r := s.approvals[s.approvalOrder[i]]
		if r.view.Actor == actor && r.view.Session == session && r.key == key && r.action != policy.ActionSave && r.view.Status != "consumed" {
			return r.tunnel, true
		}
	}
	return model.Tunnel{}, false
}

func (s *Engine) expireApprovalsLocked() error {
	for _, id := range s.approvalOrder {
		r := s.approvals[id]
		if r.view.Status != "pending" || s.clock.Now().Before(r.view.ExpiresAt) {
			continue
		}
		if err := s.recordAudit(audit.Event{Actor: audit.Actor{Kind: audit.ActorDaemon, Name: "daemon"}, Action: audit.ApprovalTimeout, TunnelID: r.view.TunnelID, ApprovalID: id, Fingerprint: r.view.Fingerprint, Decision: audit.Deny, Result: audit.ResultExpired}); err != nil {
			return err
		}
		r.view.Status = "timed_out"
		r.terminalAt = r.view.ExpiresAt
	}
	s.pruneApprovalsLocked()
	return nil
}
func (s *Engine) pruneApprovalsLocked() {
	kept := s.approvalOrder[:0]
	for _, id := range s.approvalOrder {
		r := s.approvals[id]
		if (s.sessions[r.view.Session].Connections == 0 && r.view.Status != "pending") || (!r.terminalAt.IsZero() && !s.clock.Now().Before(r.terminalAt.Add(ApprovalRetention))) {
			delete(s.approvals, id)
		} else {
			kept = append(kept, id)
		}
	}
	s.approvalOrder = kept
}
func approvalView(r *approvalRequest, actor api.Actor) api.Approval {
	view := r.view
	view.Argv = slices.Clone(view.Argv)
	view.Choices = slices.Clone(view.Choices)
	if actor.Kind == "mcp" {
		view.Code = ""
		view.Argv = nil
		view.Session = ""
	}
	return view
}
func (s *Engine) ListApprovals(actor api.Actor, session string) ([]api.Approval, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.expireApprovalsLocked(); err != nil {
		return nil, err
	}
	out := []api.Approval{}
	for _, id := range s.approvalOrder {
		r := s.approvals[id]
		if actor.Kind != "mcp" || (r.view.Actor == actor && r.view.Session == session) {
			out = append(out, approvalView(r, actor))
		}
	}
	return out, nil
}
func (s *Engine) GetApproval(actor api.Actor, session, id string) (api.Approval, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.expireApprovalsLocked(); err != nil {
		return api.Approval{}, err
	}
	r := s.approvals[id]
	if r == nil || (actor.Kind == "mcp" && (r.view.Actor != actor || r.view.Session != session)) {
		return api.Approval{}, api.NewError(api.NotFound, "approval not found")
	}
	return approvalView(r, actor), nil
}
func (s *Engine) DecideApproval(actor api.Actor, p api.DecideApprovalParams) (api.Approval, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if actor.Kind != "cli" && actor.Kind != "gui" {
		return api.Approval{}, s.policyDenied(actor, "")
	}
	if err := s.lifetimeError(); err != nil {
		return api.Approval{}, err
	}
	if err := s.expireApprovalsLocked(); err != nil {
		return api.Approval{}, err
	}
	r := s.approvals[p.ID]
	if r == nil {
		return api.Approval{}, api.NewError(api.NotFound, "approval not found")
	}
	if r.view.Status != "pending" {
		return api.Approval{}, api.NewError(api.PolicyDenied, "approval is no longer pending")
	}
	if subtle.ConstantTimeCompare([]byte(p.Code), []byte(r.view.Code)) != 1 {
		return api.Approval{}, api.NewError(api.PolicyDenied, "incorrect approval code")
	}
	if p.Decision != "approve" && p.Decision != "deny" {
		return api.Approval{}, api.NewError(api.Unknown, "decision must be approve or deny")
	}
	if !slices.Contains(r.view.Choices, p.Scope) {
		return api.Approval{}, api.NewError(api.PolicyDenied, "approval scope is not allowed")
	}
	if p.Decision == "approve" && p.Scope == "always" {
		cfg := s.agentSettings()
		current := r.tunnel
		if r.action == policy.ActionOpenSaved || r.action == policy.ActionStartSaved {
			current = model.Tunnel{}
			for _, t := range s.savedConfig().Tunnels {
				if t.ID == r.tunnel.ID {
					current = t
					break
				}
			}
		}
		spec, err := s.manager.BuildDraft(current)
		if err != nil || spec.Fingerprint() != r.view.Fingerprint {
			return api.Approval{}, api.NewError(api.PolicyDenied, "command changed; request a new approval")
		}
		if r.action != policy.ActionOpenSaved && r.action != policy.ActionStartSaved && current.SSH != nil && !s.builtAliasExists(current.SSH.Host, spec) {
			return api.Approval{}, s.policyDenied(actor, r.view.TunnelID)
		}
		if !policy.CanAlwaysAllow(r.view.Actor, r.action, current, cfg) {
			return api.Approval{}, s.policyDenied(actor, r.view.TunnelID)
		}
		// Refuse persistent policy mutation if its audit admission is unavailable.
		if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.PolicyUpdate, TunnelID: r.view.TunnelID, ApprovalID: p.ID, Fingerprint: r.view.Fingerprint, Decision: audit.Allow, ApprovalScope: audit.ScopeAlways, Result: audit.ResultOK}); err != nil {
			return api.Approval{}, err
		}
		err = s.updateSettings(func(next *settings.Settings) {
			a := next.AgentPolicy()
			a.AllowTunnels = slices.Clone(a.AllowTunnels)
			a.AllowSSHHosts = slices.Clone(a.AllowSSHHosts)
			switch r.action {
			case policy.ActionOpenSaved, policy.ActionStartSaved:
				a.AllowTunnels = append(a.AllowTunnels, literalTunnelPattern(r.tunnel.ID))
			default:
				a.AllowSSHHosts = append(a.AllowSSHHosts, r.tunnel.SSH.Host)
			}
			next.Agent = &a
		})
		if err != nil {
			return api.Approval{}, err
		}
	}
	decision, result := audit.Allow, audit.ResultOK
	if p.Decision == "deny" {
		decision, result = audit.Deny, audit.ResultDenied
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.ApprovalDecide, TunnelID: r.view.TunnelID, ApprovalID: p.ID, Fingerprint: r.view.Fingerprint, Decision: decision, ApprovalScope: audit.ApprovalScope(p.Scope), Result: result}); err != nil {
		return api.Approval{}, err
	}
	r.view.Scope = p.Scope
	r.view.Status = "approved"
	if p.Decision == "deny" {
		r.view.Status = "denied"
		r.terminalAt = s.clock.Now()
	}
	return approvalView(r, actor), nil
}

func literalTunnelPattern(id string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[").Replace(id)
}

// Summary contains only daemon-authored policy context and quoted execution
// destinations/secret references. The agent's display name lives separately.
func approvalSummary(action policy.Action, t model.Tunnel, reason string) string {
	destination := ""
	switch t.Type {
	case model.TypeSSH:
		if t.SSH != nil {
			destination = t.SSH.Hostname
			if t.SSH.Host != "" {
				destination = t.SSH.Host
			}
		}
	case model.TypeNgrok:
		if t.Ngrok != nil {
			destination = t.Ngrok.Addr
			if t.Ngrok.Domain != "" {
				destination += " (domain " + t.Ngrok.Domain + ")"
			}
		}
	case model.TypeCloudflared:
		if t.Cloudflared != nil {
			destination = t.Cloudflared.Name + " " + t.Cloudflared.URL + " " + t.Cloudflared.Hostname
		}
	}
	names := t.Secrets()
	slices.Sort(names)
	return fmt.Sprintf("%s: %s; destination %q; referenced secrets %q", action, reason, destination, names)
}
