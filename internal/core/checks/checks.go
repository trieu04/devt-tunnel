// Package checks produces the editor's findings for a tunnel definition.
package checks

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	OK      Severity = "ok"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Field    string   `json:"field,omitempty"`
}

type Deps struct {
	Resolve            func(tool string) (string, error)
	SkipSecretPresence bool // MCP dry runs must never query the keychain.
	Secrets            secret.Store
	Shell              []string
	Home               string
	Dialect            command.Dialect
	SSHConfig          string
	ProfileID          string
	Hosts              func(profileID string) ([]string, error)
	Listening          func(address string) bool
}

// Run checks t against the rest of the config.
func Run(t model.Tunnel, cfg model.Config, d Deps) []Finding {
	var out []Finding
	spec, err := command.Build(t, command.Options{Shell: d.Shell, Home: d.Home, Dialect: d.Dialect, SSHConfig: d.SSHConfig})
	if err != nil {
		return []Finding{{Severity: Error, Message: err.Error(), Field: buildErrorField(err)}}
	}
	if path, err := d.Resolve(spec.Tool); err != nil {
		out = append(out, Finding{Severity: Error, Message: fmt.Sprintf("%s was not found on this machine", spec.Tool), Field: "type"})
	} else {
		out = append(out, Finding{Severity: OK, Message: fmt.Sprintf("%s found at %s", spec.Tool, path), Field: "type"})
	}
	if t.Type == model.TypeSSH {
		out = append(out, sshFindings(t, d)...)
	}
	if !d.SkipSecretPresence {
		out = append(out, secretFindings(t, d)...)
	}
	out = append(out, portFindings(t, cfg, d)...)
	return out
}

// sshFindings reports risky or unsupported SSH options and whether the host alias is known.
func sshFindings(t model.Tunnel, d Deps) []Finding {
	var out []Finding
	if len(t.Forwards) == 0 {
		out = append(out, Finding{Severity: Warning, Message: "SSH tunnel has no port forwards", Field: "forwards"})
	}
	if !slices.Contains(t.SSH.Flags, "-N") {
		out = append(out, Finding{Severity: Warning, Message: "without -N, ssh would open a remote shell", Field: "ssh.flags"})
	}
	if slices.Contains(t.SSH.Flags, "-A") {
		out = append(out, Finding{Severity: Warning, Message: "agent forwarding exposes the agent to the remote host", Field: "ssh.flags"})
	}
	allArgs := append(slices.Clone(t.SSH.Flags), strings.Fields(t.SSH.ExtraArgs)...)
	if slices.Contains(allArgs, "-f") {
		out = append(out, Finding{Severity: Error, Message: "-f is not supported because it detaches from supervision", Field: "ssh.extraArgs"})
	}
	if slices.Contains(allArgs, "-M") || slices.Contains(allArgs, "-S") || t.SSH.Options["ControlMaster"] != "" {
		out = append(out, Finding{Severity: Warning, Message: "SSH connection sharing can move forwards outside the supervised process", Field: "ssh.extraArgs"})
	}
	if t.SSH.Options["ServerAliveInterval"] == "" {
		out = append(out, Finding{Severity: Warning, Message: "set ServerAliveInterval for faster recovery after network changes", Field: "ssh.options.ServerAliveInterval"})
	}
	if t.SSH.Host != "" && d.Hosts != nil {
		hosts, err := d.Hosts(d.ProfileID)
		if err != nil {
			out = append(out, Finding{Severity: Warning, Message: "SSH host list could not be read: " + err.Error(), Field: "ssh.host"})
		} else if slices.Contains(hosts, t.SSH.Host) {
			out = append(out, Finding{Severity: OK, Message: fmt.Sprintf("SSH host %s found", t.SSH.Host), Field: "ssh.host"})
		} else {
			out = append(out, Finding{Severity: Warning, Message: fmt.Sprintf("%s is not a literal alias in the SSH config; ssh may still resolve it", t.SSH.Host), Field: "ssh.host"})
		}
	}
	return out
}

// secretFindings reports whether every secret the tunnel names is in the store.
func secretFindings(t model.Tunnel, d Deps) []Finding {
	var out []Finding
	for _, name := range t.Secrets() {
		_, err := secret.Has(d.Secrets, name)
		switch {
		case errors.Is(err, secret.ErrNotFound):
			out = append(out, Finding{Severity: Error, Message: fmt.Sprintf("secret %q is missing", name), Field: "secret"})
		case err != nil:
			out = append(out, Finding{Severity: Error, Message: "the OS keychain is unavailable", Field: "secret"})
		default:
			out = append(out, Finding{Severity: OK, Message: fmt.Sprintf("secret %q is present", name), Field: "secret"})
		}
	}
	return out
}

// portFindings reports listener ports that clash with other tunnels or with a process already listening.
func portFindings(t model.Tunnel, cfg model.Config, d Deps) []Finding {
	var out []Finding
	for i, port := range listenerPorts(t) {
		conflict := false
		for _, other := range cfg.Tunnels {
			if other.ID == t.ID {
				continue
			}
			for _, candidate := range listenerPorts(other) {
				if portsConflict(port, candidate) {
					conflict = true
					out = append(out, Finding{Severity: Warning, Message: fmt.Sprintf("%s conflicts with %s", port, other.Name), Field: fmt.Sprintf("forwards[%d].bind", i)})
				}
			}
		}
		if !conflict && d.Listening != nil && d.Listening(command.DialAddress(port)) {
			conflict = true
			out = append(out, Finding{Severity: Warning, Message: fmt.Sprintf("%s is already in use", port), Field: fmt.Sprintf("forwards[%d].bind", i)})
		}
		if !conflict {
			out = append(out, Finding{Severity: OK, Message: fmt.Sprintf("%s is available", port), Field: fmt.Sprintf("forwards[%d].bind", i)})
		}
		for _, other := range cfg.Tunnels {
			if other.ID == t.ID {
				continue
			}
			for _, target := range targetPorts(other) {
				if portsConflict(port, target) {
					out = append(out, Finding{Severity: Warning, Message: fmt.Sprintf("%s also connects to %s", other.Name, port), Field: fmt.Sprintf("forwards[%d].bind", i)})
				}
			}
		}
	}
	return out
}

func listenerPorts(t model.Tunnel) []string {
	var ports []string
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardRemote {
			ports = append(ports, f.Bind)
		}
	}
	return ports
}

func targetPorts(t model.Tunnel) []string {
	if t.Type == model.TypeNgrok && t.Ngrok != nil {
		return []string{targetAddress(t.Ngrok.Addr)}
	}
	if t.Type == model.TypeCloudflared && t.Cloudflared != nil && t.Cloudflared.URL != "" {
		return []string{targetAddress(t.Cloudflared.URL)}
	}
	return nil
}

func targetAddress(value string) string {
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return strings.TrimPrefix(strings.TrimPrefix(value, "http://"), "https://")
}

func portsConflict(a, b string) bool {
	ah, ap := splitAddress(command.DialAddress(a))
	bh, bp := splitAddress(command.DialAddress(b))
	return ap == bp && (ah == bh || wildcardHost(a) || wildcardHost(b))
}

func splitAddress(value string) (string, string) {
	h, p, _ := net.SplitHostPort(value)
	return h, p
}

func wildcardHost(value string) bool {
	h, _, err := net.SplitHostPort(value)
	return err == nil && (h == "" || h == "*" || h == "0.0.0.0" || h == "::")
}

func buildErrorField(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "extra_args"):
		return "extraArgs"
	case strings.Contains(message, "ready_log"):
		return "command.readyLog"
	case strings.Contains(message, "forward"):
		return "forwards"
	}
	return "type"
}
