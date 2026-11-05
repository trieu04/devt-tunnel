// Package policy implements the MCP policy table as pure decisions. It does
// not authenticate actors, look up aliases, track lease ownership, or grant
// approvals. The daemon must supply those facts and enforce the result.
package policy

import (
	"net"
	"path"
	"strconv"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
)

// ActorKind aliases the model's wire representation so policy and leases use
// the same actor without conversions or a dependency on the API/daemon.
type ActorKind = string

const (
	ActorMCP ActorKind = "mcp"
	ActorCLI ActorKind = "cli"
	ActorGUI ActorKind = "gui"
)

// Actor is a client-declared identity, not a security boundary against a
// same-user client with shell access. Name is for display/audit, not privilege.
type Actor = model.Actor

type Action string

const (
	ActionListTunnels  Action = "list_tunnels"
	ActionListSSHHosts Action = "list_ssh_hosts"
	ActionReadLogs     Action = "read_logs" // MCP logs must already be redacted.
	ActionCheck        Action = "check"     // Dry run; never execute the spec.
	ActionListLeases   Action = "list_leases"
	ActionCloseLease   Action = "close_lease" // Only the caller's own lease.
	ActionRenewLease   Action = "renew_lease" // Only the caller's own lease.

	// Saved means a definition loaded by the daemon from its saved registry,
	// NOT an agent-supplied spec with an ID that happens to match a saved ID.
	ActionOpenSaved  Action = "open_saved"
	ActionStartSaved Action = "start_saved" // MCP starts still acquire a lease.
	ActionOpen       Action = "open"        // New/ephemeral agent-built spec.
	ActionCreate     Action = "create"      // New/ephemeral agent-built spec.
	ActionExpose     Action = "expose"      // Always requires public consent.
	ActionSave       Action = "save"        // Save/promote a new definition, not edit an existing one.
	ActionUseSecret  Action = "use_secret"  // Reference an existing secret by name in a new spec.

	ActionCreateCommand Action = "create_command"
	ActionEditCommand   Action = "edit_command"
	ActionSetFlags      Action = "set_flags"
	ActionSetOptions    Action = "set_options"
	ActionSetExtraArgs  Action = "set_extra_args"
	ActionSetIdentity   Action = "set_identity"
	ActionReadSecret    Action = "read_secret"
	ActionSetSecret     Action = "set_secret"
	ActionRenameSecret  Action = "rename_secret"
	ActionDeleteSecret  Action = "delete_secret"
	ActionAnswerPrompt  Action = "answer_prompt"
	ActionEditPolicy    Action = "edit_policy"
	ActionEditSaved     Action = "edit_saved"
	ActionDeleteSaved   Action = "delete_saved"
	ActionStop          Action = "stop" // Direct stop, as distinct from releasing one's own lease.
)

type Decision string

const (
	Allow Decision = "allow"
	Ask   Decision = "ask"
	Deny  Decision = "deny"
)

// Decide implements policy without I/O or mutation. Settings loaded without an
// [agent] section and zero in-memory Settings both use settings.DefaultAgent.
// The daemon must distinguish saved opens from new specs, verify the existence
// of config aliases, and check ownership before using CloseLease/RenewLease.
// An Ask is not permission to spawn: run only the approved Spec.Fingerprint.
func Decide(actor Actor, action Action, t model.Tunnel, cfg settings.Settings) (Decision, string) {
	if !knownAction(action) {
		return Deny, "unknown action"
	}
	switch actor.Kind {
	case ActorCLI, ActorGUI:
		return Allow, "person-controlled client; MCP policy does not apply"
	case ActorMCP:
	default:
		return Deny, "unknown actor kind"
	}
	a := cfg.AgentPolicy()
	if err := a.Validate(); err != nil {
		return Deny, "invalid agent policy: " + err.Error()
	}
	if !a.Enabled {
		return Deny, "agent access is disabled"
	}
	switch action {
	case ActionListTunnels, ActionListSSHHosts, ActionReadLogs, ActionCheck, ActionListLeases:
		return Allow, "read-only agent action"
	case ActionCloseLease, ActionRenewLease:
		return Allow, "agent may release or renew its own lease"
	case ActionCreateCommand, ActionEditCommand, ActionSetFlags, ActionSetOptions, ActionSetExtraArgs, ActionSetIdentity:
		return Deny, "agents cannot create or edit execution extensions"
	case ActionReadSecret, ActionSetSecret, ActionRenameSecret, ActionDeleteSecret,
		ActionAnswerPrompt, ActionEditPolicy, ActionEditSaved, ActionDeleteSaved, ActionStop:
		return Deny, "action is reserved for a person"
	case ActionUseSecret:
		return Ask, "using an existing secret for a new tunnel requires approval"
	}

	saved := action == ActionOpenSaved || action == ActionStartSaved
	unsafe := agentError(t)
	if !saved && unsafe != "" {
		return Deny, "unsafe agent-built tunnel: " + unsafe
	}
	// Public exposure overrides every allow-list, including a saved tunnel ID.
	// Every remote forward is public for policy even when its bind is loopback.
	if action == ActionExpose || PublicExposure(t) {
		if a.Public == "deny" {
			return Deny, "public exposure is disabled by agent policy"
		}
		return Ask, "public exposure always requires a person; never allow always"
	}
	if saved {
		if unsafe != "" {
			return Ask, "unsafe saved tunnel requires approval; never allow always: " + unsafe
		}
		if matchesTunnel(a.AllowTunnels, t.ID) {
			return Allow, "saved tunnel matches allow_tunnels"
		}
		return Ask, "saved tunnel is not in allow_tunnels"
	}
	if action == ActionSave {
		return Ask, "saving a tunnel requires approval"
	}
	if len(t.Secrets()) > 0 {
		return Ask, "using an existing secret for a new tunnel requires approval"
	}
	if localAliasForward(t) {
		for _, alias := range a.AllowSSHHosts {
			if alias == t.SSH.Host {
				return Allow, "local forward alias is in allow_ssh_hosts"
			}
		}
		return Ask, "SSH alias is not in allow_ssh_hosts"
	}
	return Ask, "new tunnel requires approval; manual hosts cannot be allow-listed"
}

// CanAlwaysAllow tells approval UIs whether they may offer an "always" grant.
// Only a safe saved tunnel ID or a safe new 127.0.0.1 L/D alias forward can
// produce an allow-list entry. Public and unsafe saved tunnels, new manual
// hosts, new secret references and saves can never gain persistent approval.
func CanAlwaysAllow(actor Actor, action Action, t model.Tunnel, cfg settings.Settings) bool {
	decision, _ := Decide(actor, action, t, cfg)
	if decision != Ask || action == ActionExpose || PublicExposure(t) || agentError(t) != "" {
		return false
	}
	switch action {
	case ActionOpenSaved, ActionStartSaved:
		return t.ID != ""
	case ActionOpen, ActionCreate:
		return len(t.Secrets()) == 0 && localAliasForward(t)
	}
	return false
}

func agentError(t model.Tunnel) string {
	for _, finding := range checks.AgentSafe(t) {
		if finding.Severity == checks.Error {
			return finding.Message
		}
	}
	return ""
}

func matchesTunnel(patterns []string, id string) bool {
	if id == "" {
		return false
	}
	for _, pattern := range patterns {
		if match, err := path.Match(pattern, id); err == nil && match {
			return true
		}
	}
	return false
}

func localAliasForward(t model.Tunnel) bool {
	if t.Type != model.TypeSSH || t.SSH == nil || t.SSH.Host == "" || t.SSH.Hostname != "" || len(t.Forwards) == 0 {
		return false
	}
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardLocal && f.Kind != model.ForwardDynamic {
			return false
		}
		host, port, err := net.SplitHostPort(f.Bind)
		if err != nil || host != "127.0.0.1" || !validPort(port) {
			return false
		}
	}
	return true
}

// PublicExposure is conservative and performs no DNS lookups. Ngrok,
// cloudflared, every remote forward and every non-loopback listener require
// public approval. Unix sockets and bare SSH ports are not public; bare ports
// use SSH's default loopback bind, but are not eligible for alias allow-listing.
// Unknown/malformed addresses are treated as public, never as an allow grant.
func PublicExposure(t model.Tunnel) bool {
	if t.Type == model.TypeNgrok || t.Type == model.TypeCloudflared {
		return true
	}
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardLocal && f.Kind != model.ForwardDynamic {
			return true
		}
		if strings.Contains(f.Bind, "/") { // OpenSSH Unix socket bind.
			continue
		}
		if validPort(f.Bind) {
			continue
		}
		host, port, err := net.SplitHostPort(f.Bind)
		if err != nil || !validPort(port) {
			return true
		}
		if strings.EqualFold(host, "localhost") {
			continue
		}
		if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
			host = host[:zone]
		}
		if !net.ParseIP(host).IsLoopback() {
			return true
		}
	}
	return false
}

func validPort(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && n <= 65535
}

func knownAction(action Action) bool {
	switch action {
	case ActionListTunnels, ActionListSSHHosts, ActionReadLogs, ActionCheck, ActionListLeases,
		ActionCloseLease, ActionRenewLease, ActionOpenSaved, ActionStartSaved, ActionOpen,
		ActionCreate, ActionExpose, ActionSave, ActionUseSecret, ActionCreateCommand,
		ActionEditCommand, ActionSetFlags, ActionSetOptions, ActionSetExtraArgs, ActionSetIdentity,
		ActionReadSecret, ActionSetSecret, ActionRenameSecret, ActionDeleteSecret,
		ActionAnswerPrompt, ActionEditPolicy, ActionEditSaved, ActionDeleteSaved, ActionStop:
		return true
	}
	return false
}
