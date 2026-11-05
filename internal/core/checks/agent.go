package checks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// agentSafeSSHOptions is deliberately an allow-list, not a dangerous-option
// block-list. The first three entries mirror command.buildSSH's fixed defaults.
// Keepalives and -N are also accepted because engine.NewDraft and the MCP spec
// add them (the draft sets ServerAliveInterval=30). Positive plain integers are
// safe for both OpenSSH keepalive settings. Everything else, including
// ProxyJump, is outside the agent schema and requires a human-authored tunnel.
var agentSafeSSHOptions = map[string]func(string) bool{
	"exitonforwardfailure": func(v string) bool { return strings.EqualFold(v, "yes") },
	"controlmaster":        func(v string) bool { return strings.EqualFold(v, "no") },
	"controlpath":          func(v string) bool { return strings.EqualFold(v, "none") },
	"serveraliveinterval":  positiveInteger,
	"serveralivecountmax":  positiveInteger,
}

func positiveInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0
}

// AgentSafe checks the agent-controlled fields without I/O or ssh_config
// inspection. The owner's config aliases are trusted. This is intentionally
// stricter than the editor's checks: no arbitrary extra_args are accepted,
// even whitespace or apparently harmless options. Rejecting the whole channel
// covers all quoting, dialect and case variants of -oProxyCommand, -o followed
// by ProxyCommand[=...], LocalCommand, PermitLocalCommand, -Fpath, a dangling
// -o, and options not yet known to this version. Do not replace this with a
// blacklist: SSH's option syntax and configuration include command execution.
//
// For new agent-built specs, error findings mean deny. For opening a saved,
// human-authored definition, policy requires non-persistent approval instead.
func AgentSafe(t model.Tunnel) []Finding {
	var out []Finding
	reject := func(field, message string) {
		out = append(out, Finding{Severity: Error, Field: field, Message: message})
	}
	switch t.Type {
	case model.TypeCommand:
		reject("type", "agents cannot build command tunnels")
	case model.TypeSSH:
		if t.SSH == nil {
			reject("ssh", "SSH settings are missing")
		}
	case model.TypeNgrok:
		if t.Ngrok == nil {
			reject("ngrok", "ngrok settings are missing")
		}
	case model.TypeCloudflared:
		if t.Cloudflared == nil {
			reject("cloudflared", "cloudflared settings are missing")
		}
	default:
		reject("type", "unsupported agent tunnel type")
	}
	// Inspect all present sections, even an inactive one: switching the type
	// must not smuggle an unchecked extension through a previously safe spec.
	if c := t.SSH; c != nil {
		for _, flag := range c.Flags {
			if flag != "-N" {
				reject("ssh.flags", "agents may use only the standard -N SSH flag")
				break
			}
		}
		keys := make([]string, 0, len(c.Options))
		for key := range c.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys) // Findings and policy reasons must be deterministic.
		for _, key := range keys {
			check, ok := agentSafeSSHOptions[strings.ToLower(key)]
			if !ok || !check(c.Options[key]) {
				// Do not reflect supplied option names or values into diagnostics.
				reject("ssh.options", "SSH option is outside the agent-safe defaults")
			}
		}
		if c.ExtraArgs != "" {
			reject("ssh.extraArgs", "agents cannot supply SSH extra arguments")
		}
		if c.Identity != "" {
			reject("ssh.identity", "agents cannot supply an SSH identity path")
		}
		for _, destination := range []struct{ field, value string }{
			{"host", c.Host}, {"hostname", c.Hostname}, {"user", c.User},
		} {
			if strings.HasPrefix(destination.value, "-") || strings.ContainsAny(destination.value, " \t\r\n\x00") {
				reject("ssh."+destination.field, fmt.Sprintf("SSH %s must not contain options or whitespace", destination.field))
			}
		}
	}
	if c := t.Ngrok; c != nil && c.ExtraArgs != "" {
		reject("ngrok.extraArgs", "agents cannot supply ngrok extra arguments")
	}
	if c := t.Cloudflared; c != nil && c.ExtraArgs != "" {
		reject("cloudflared.extraArgs", "agents cannot supply cloudflared extra arguments")
	}
	return out
}
