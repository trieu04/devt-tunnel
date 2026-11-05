package checks

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
		if c.PassphraseSecret != "" && (c.Host == "" || c.Hostname != "") {
			reject("ssh.passphraseSecret", "agents may reference an SSH passphrase secret only with a config alias, not a manual hostname")
		}
		if c.Hostname != "" && !agentHostname(c.Hostname) {
			reject("ssh.hostname", "manual SSH hostname contains unsupported characters")
		}
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
			if strings.HasPrefix(destination.value, "-") || strings.ContainsFunc(destination.value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				reject("ssh."+destination.field, fmt.Sprintf("SSH %s must not contain options or whitespace", destination.field))
			}
		}
	}
	for _, f := range t.Forwards {
		for _, address := range []struct{ field, value string }{{"bind", f.Bind}, {"target", f.Target}} {
			if address.value != "" && !agentForwardAddress(address.value) {
				reject("forwards."+address.field, "agent forward addresses must be a port, host:port, strict bracketed IPv6:port, or an absolute/relative Unix socket path without colons")
			}
		}
	}
	if c := t.Ngrok; c != nil {
		if c.ExtraArgs != "" {
			reject("ngrok.extraArgs", "agents cannot supply ngrok extra arguments")
		}
		for _, v := range []string{c.Addr, c.Domain} {
			if unsafeArgument(v) {
				reject("ngrok", "ngrok addresses and domains must not contain options or whitespace")
			}
		}
	}
	if c := t.Cloudflared; c != nil {
		if c.ExtraArgs != "" {
			reject("cloudflared.extraArgs", "agents cannot supply cloudflared extra arguments")
		}
		for _, v := range []string{c.Name, c.URL, c.Hostname} {
			if unsafeArgument(v) {
				reject("cloudflared", "cloudflared names and addresses must not contain options or whitespace")
			}
		}
	}
	return out
}

// Agent addresses are passed as one OpenSSH forwarding argument, not as shell
// text. Reject extra OpenSSH delimiters even when a slash would otherwise make
// the editor treat the value as a socket. IPv6 is accepted only after parsing.
func agentForwardAddress(value string) bool {
	if unsafeArgument(value) {
		return false
	}
	if (strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./")) && !strings.ContainsAny(value, ":[]") {
		return true
	}
	if positivePort(value) {
		return true
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || !positivePort(port) {
		return false
	}
	if strings.ContainsAny(value, "[]") {
		return strings.HasPrefix(value, "[") && strings.Count(value, "[") == 1 && strings.Count(value, "]") == 1 && strings.Contains(host, ":") && net.ParseIP(host) != nil
	}
	return strings.Count(value, ":") == 1 && !strings.ContainsAny(host, "/[]")
}
func positivePort(value string) bool {
	n, err := strconv.Atoi(value)
	return positiveInteger(value) && err == nil && n <= 65535
}
func unsafeArgument(value string) bool {
	return strings.HasPrefix(value, "-") || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func agentHostname(value string) bool {
	// Brackets are supported only for a parsed IPv6 literal, never arbitrary
	// bracket syntax. Bare IPv6 is safe as an SSH destination (not a forward).
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return strings.Contains(value, ":") && net.ParseIP(value[1:len(value)-1]) != nil
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return !strings.HasPrefix(value, "-")
}
