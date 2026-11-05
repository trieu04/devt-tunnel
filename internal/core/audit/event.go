// Package audit records state-changing calls and approval decisions as JSONL.
// Callers explicitly construct Events: never serialize requests, command specs,
// errors or process output into the audit trail. Read-only log access is not an
// audit event. The daemon owns one Writer for a state directory; multiple
// goroutines may share it, but multiple writers/processes are not supported.
//
// This is an owner-readable operational record, NOT a tamper-proof security
// boundary. An agent running as the same OS user can change or remove it.
package audit

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type ActorKind string

const (
	ActorGUI    ActorKind = "gui"
	ActorCLI    ActorKind = "cli"
	ActorMCP    ActorKind = "mcp"
	ActorDaemon ActorKind = "daemon" // expiry, disconnect cleanup and recovery
)

type Actor struct {
	Kind ActorKind `json:"kind"`
	Name string    `json:"name"`
}

// Action is a closed vocabulary. Add a constant and validation entry when a new
// state-changing API is wired in; do not put raw method/request text here.
type Action string

const (
	TunnelStart         Action = "tunnel.start"
	TunnelStop          Action = "tunnel.stop"
	TunnelRestart       Action = "tunnel.restart"
	TunnelSave          Action = "tunnel.save"
	TunnelDelete        Action = "tunnel.delete"
	TunnelPromote       Action = "tunnel.promote"
	TunnelImport        Action = "tunnel.import"
	LeaseOpen           Action = "lease.open"
	LeaseRenew          Action = "lease.renew"
	LeaseClose          Action = "lease.close"
	LeaseExpire         Action = "lease.expire"
	LeaseRevoke         Action = "lease.revoke"
	ApprovalRequest     Action = "approval.request"
	ApprovalTimeout     Action = "approval.timeout"
	PolicyDeny          Action = "policy.deny"
	TunnelRunEphemeral  Action = "tunnel.runEphemeral"
	TunnelStopEphemeral Action = "tunnel.stopEphemeral"
	ApprovalDecide      Action = "approval.decide"
	SecretSet           Action = "secret.set"
	SecretRename        Action = "secret.rename"
	SecretDelete        Action = "secret.delete"
	SecretForget        Action = "secret.forget"
	PromptAnswer        Action = "prompt.answer"
	SettingsUpdate      Action = "settings.update"
	PolicyUpdate        Action = "policy.update"
	ProfileSave         Action = "profile.save"
	ProfileDelete       Action = "profile.delete"
	ProfileActivate     Action = "profile.activate"
	ProfileStart        Action = "profile.start"
	ProfileStop         Action = "profile.stop"
	ConfigReload        Action = "config.reload"
	OrphanStop          Action = "orphan.stop"
	DaemonShutdown      Action = "daemon.shutdown"
)

type Decision string

const (
	Allow Decision = "allow"
	Ask   Decision = "ask"
	Deny  Decision = "deny"
)

// Result is a status code, never an error message or process output.
type Result string

const (
	ResultOK              Result = "ok"
	ResultPendingApproval Result = "pending_approval"
	ResultDenied          Result = "denied"
	ResultFailed          Result = "failed"
	ResultExpired         Result = "expired"
	ResultRevoked         Result = "revoked"
	ResultNeedsUser       Result = "needs_user"
	ResultNeedsSecret     Result = "needs_secret"
	ResultConflict        Result = "conflict"
	ResultUnknown         Result = "unknown"
)

type ApprovalScope string

const (
	ScopeOnce    ApprovalScope = "once"
	ScopeSession ApprovalScope = "session"
	ScopeAlways  ApprovalScope = "always"
)

// Event deliberately has NO free-form message, purpose, summary, argv, payload,
// log, error or secret-value field. Secret references are NAMES ONLY. Variable
// strings below are identifiers/labels, not containers for request data. Their
// validation cannot distinguish an identifier from a secret deliberately placed
// in that identifier: callers must not copy sensitive text into metadata.
//
// Sequence and Time are assigned by Append (Time uses the injectable clock).
// Sequence is a persistent, monotonic reader offset, independent of filenames
// and timestamps, including across rotation and writer restart.
type Event struct {
	Count         uint64        `json:"count,omitempty"` // coalesced policy.deny calls, not request text
	Sequence      uint64        `json:"seq"`
	Time          time.Time     `json:"time"`
	Actor         Actor         `json:"actor"`
	Action        Action        `json:"action"`
	TunnelID      string        `json:"tunnel_id,omitempty"`
	LeaseID       string        `json:"lease_id,omitempty"`
	RequestID     string        `json:"request_id,omitempty"`
	ApprovalID    string        `json:"approval_id,omitempty"`
	Fingerprint   string        `json:"fingerprint,omitempty"` // SHA-256 hex, not argv
	SecretNames   []string      `json:"secret_names,omitempty"`
	Decision      Decision      `json:"decision,omitempty"`
	ApprovalScope ApprovalScope `json:"approval_scope,omitempty"`
	Result        Result        `json:"result"`
}

var ErrInvalidEvent = errors.New("audit: invalid event")

// Validate checks the closed vocabulary and bounds identifier metadata. Errors
// name the invalid field but deliberately never repeat its value.
func (e Event) Validate() error {
	invalid := func(field string) error { return fmt.Errorf("%w: %s", ErrInvalidEvent, field) }
	switch e.Actor.Kind {
	case ActorGUI, ActorCLI, ActorMCP, ActorDaemon:
	default:
		return invalid("actor.kind")
	}
	if !ValidActorName(e.Actor.Name) {
		return invalid("actor.name")
	}
	switch e.Action {
	case TunnelStart, TunnelStop, TunnelRestart, TunnelSave, TunnelDelete, TunnelPromote,
		TunnelImport, LeaseOpen, LeaseRenew, LeaseClose, LeaseExpire, LeaseRevoke,
		ApprovalRequest, ApprovalTimeout, PolicyDeny, TunnelRunEphemeral, TunnelStopEphemeral, ApprovalDecide, SecretSet, SecretRename, SecretDelete, SecretForget,
		PromptAnswer, SettingsUpdate, PolicyUpdate, ProfileSave, ProfileDelete,
		ProfileActivate, ProfileStart, ProfileStop, ConfigReload, OrphanStop, DaemonShutdown:
	default:
		return invalid("action")
	}
	switch e.Decision {
	case "", Allow, Ask, Deny:
	default:
		return invalid("decision")
	}
	switch e.Result {
	case ResultOK, ResultPendingApproval, ResultDenied, ResultFailed, ResultExpired,
		ResultRevoked, ResultNeedsUser, ResultNeedsSecret, ResultConflict, ResultUnknown:
	default:
		return invalid("result")
	}
	switch e.ApprovalScope {
	case "", ScopeOnce, ScopeSession, ScopeAlways:
	default:
		return invalid("approval_scope")
	}
	for _, field := range []struct{ name, value string }{
		{"tunnel_id", e.TunnelID}, {"lease_id", e.LeaseID},
		{"request_id", e.RequestID}, {"approval_id", e.ApprovalID},
	} {
		if !identifier(field.value, false) {
			return invalid(field.name)
		}
	}
	if e.Fingerprint != "" {
		if len(e.Fingerprint) != 64 {
			return invalid("fingerprint")
		}
		if _, err := hex.DecodeString(e.Fingerprint); err != nil {
			return invalid("fingerprint")
		}
	}
	if len(e.SecretNames) > 64 {
		return invalid("secret_names")
	}
	for _, name := range e.SecretNames {
		if !identifier(name, true) {
			return invalid("secret_names")
		}
	}
	return nil
}

func identifier(value string, required bool) bool {
	if len(value) > 256 || !utf8.ValidString(value) || (required && strings.TrimSpace(value) == "") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

// Filter implements dtt audit's --actor (exact actor name), --tunnel (exact id)
// and --since (inclusive time lower bound). A CLI converts its duration/date
// argument to Since. Empty/zero fields match all events; fields combine by AND.
type Filter struct {
	Actor  string
	Tunnel string
	Since  time.Time
}

func (f Filter) Match(e Event) bool {
	return (f.Actor == "" || e.Actor.Name == f.Actor) &&
		(f.Tunnel == "" || e.TunnelID == f.Tunnel) &&
		(f.Since.IsZero() || !e.Time.Before(f.Since))
}

// ValidActorName accepts human-facing client labels (including spaces), not
// identifier tokens. Bound printable UTF-8 prevents multiline/terminal spoofing.
func ValidActorName(value string) bool {
	if !identifier(value, true) {
		return false
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
