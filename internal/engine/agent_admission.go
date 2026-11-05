package engine

import (
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

const MaxAgentLeases = 32

// These are admission checks for NEW agent definitions, not restrictions on
// existing person-authored definitions. In particular an approval fingerprint
// binds execution, not profile membership or future GUI auto-start behavior.
// configMu must be held.
func (s *Engine) agentBuilt(actor api.Actor, session string, t model.Tunnel) error {
	if actor.Kind != "mcp" {
		return nil
	}
	if reason := s.agentBuiltReason(t); reason != "" {
		return s.denySession(actor, session, t.ID, reason)
	}
	return nil
}

// Pure admission diagnostics shared with dry runs. No approval, audit or
// mutation is performed; configMu must be held for the saved-port snapshot.
func (s *Engine) agentBuiltReason(t model.Tunnel) string {
	if t.StartOnLaunch || len(t.Profiles) != 0 || t.Favorite {
		return "agents cannot set startOnLaunch, profiles or favorite; saved and ephemeral agent tunnels must remain lease-controlled"
	}
	if !utf8.ValidString(t.Name) || utf8.RuneCountInString(t.Name) > 64 || strings.ContainsFunc(t.Name, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return "agent-provided name must be printable UTF-8, without control characters, and at most 64 characters"
	}
	for _, f := range checks.AgentSafe(t) {
		if f.Severity == checks.Error {
			return f.Message
		}
	}
	// A Bind spelling (as opposed to listen_port) must not bypass the saved
	// registry reservations. Compare ports even across different local hosts;
	// a wildcard listener could otherwise squat on a stopped saved tunnel.
	reserved := map[int]bool{}
	for _, saved := range s.savedConfig().Tunnels {
		for _, f := range saved.Forwards {
			if f.Kind != model.ForwardRemote {
				reserved[bindPort(f.Bind)] = true
			}
		}
	}
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardRemote && bindPort(f.Bind) != 0 && reserved[bindPort(f.Bind)] {
			return "agent listen port is reserved by a saved tunnel"
		}
	}
	return ""
}

func bindPort(bind string) int {
	_, port, err := net.SplitHostPort(bind)
	if err != nil {
		port = bind
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return n
}

// configMu serializes the capacity check with admission, including concurrent
// requests on different connections that resume the same session token.
func (s *Engine) agentLeaseCapacity(actor api.Actor, session string) error {
	if actor.Kind != "mcp" {
		return nil
	}
	n := 0
	for _, l := range s.runtimeSnapshot().Leases {
		if ownsLease(l, actor, session) && leaseAlive(l, s.clock.Now()) {
			n++
		}
	}
	if n >= MaxAgentLeases {
		return s.denySession(actor, session, "", "agent session has reached its limit of 32 live leases/ephemeral tunnels")
	}
	return nil
}
