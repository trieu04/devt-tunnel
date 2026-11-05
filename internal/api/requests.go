package api

import (
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// OpenRequestParams chooses exactly one daemon-loaded saved ID or inline draft.
// RequestID is scoped to the connected actor/session. TTL is a Go duration.
// CheckRequest accepts this shape too, but does not require RequestID.
type OpenRequestParams struct {
	RequestID string        `json:"requestId,omitempty"`
	ID        string        `json:"id,omitempty"`
	Tunnel    *model.Tunnel `json:"tunnel,omitempty"`
	Purpose   string        `json:"purpose"`
	TTL       string        `json:"ttl,omitempty"`
}

type WaitRequestParams struct {
	RequestID string `json:"requestId"`
}

// OpenRequestResult is a nonblocking snapshot. A pending approval is a normal
// outcome, not permission to execute: waitRequest retries the stored original
// operation through ordinary fingerprint-bound admission after approval.
type OpenRequestResult struct {
	RequestID  string     `json:"requestId"`
	Status     string     `json:"status"` // pending_approval | starting | ready | needs_user | denied | timed_out | expired | failed
	ApprovalID string     `json:"approvalId,omitempty"`
	Lease      *Lease     `json:"lease,omitempty"`
	TunnelID   string     `json:"tunnelId,omitempty"`
	Message    string     `json:"message,omitempty"`
	PromptKind string     `json:"promptKind,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Endpoints  []Endpoint `json:"endpoints"`
}

type CheckRequestResult struct {
	Decision   string           `json:"decision"`
	Reason     string           `json:"reason"`
	Preview    string           `json:"preview"`
	Findings   []checks.Finding `json:"findings"`
	BuildError string           `json:"buildError,omitempty"`
}

// AgentPolicyResult deliberately lists only the non-sensitive effective agent
// policy fields. Do not embed settings.Settings or a growing settings type.
type AgentPolicyResult struct {
	Enabled       bool     `json:"enabled"`
	Public        string   `json:"public"`
	AllowTunnels  []string `json:"allowTunnels"`
	AllowSSHHosts []string `json:"allowSSHHosts"`
	PortRange     string   `json:"portRange"`
	DefaultTTL    string   `json:"defaultTTL"`
	MaxTTL        string   `json:"maxTTL"`
}
