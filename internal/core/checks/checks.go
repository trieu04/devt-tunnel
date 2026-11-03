// Package checks produces the editor's findings for a tunnel definition.
package checks

import (
	"errors"
	"fmt"
	"slices"

	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

type Deps struct {
	Resolve func(tool string) (string, error)
	Secrets secret.Store
	Shell   []string
}

// Run checks t against the rest of the config.
func Run(t model.Tunnel, cfg model.Config, d Deps) []Finding {
	var out []Finding
	spec, err := command.Build(t, command.Options{Shell: d.Shell})
	if err != nil {
		return []Finding{{Error, err.Error()}}
	}
	if _, err := d.Resolve(spec.Tool); err != nil {
		out = append(out, Finding{Error, fmt.Sprintf("%s was not found on this machine", spec.Tool)})
	}
	if t.Type == model.TypeSSH && !slices.Contains(t.SSH.Flags, "-N") {
		out = append(out, Finding{Warning, "without -N, ssh would open a remote shell"})
	}
	for _, name := range t.Secrets() {
		_, err := d.Secrets.Get(name)
		switch {
		case errors.Is(err, secret.ErrNotFound):
			out = append(out, Finding{Error, fmt.Sprintf("secret %q is missing", name)})
		case err != nil:
			out = append(out, Finding{Error, "the OS keychain is unavailable"})
		}
	}
	for _, port := range localPorts(t) {
		for _, other := range cfg.Tunnels {
			if other.ID != t.ID && slices.Contains(localPorts(other), port) {
				out = append(out, Finding{Warning, fmt.Sprintf("%s is also used by %s", port, other.Name)})
			}
		}
	}
	return out
}

func localPorts(t model.Tunnel) []string {
	var ports []string
	for _, f := range t.Forwards {
		if f.Kind != model.ForwardRemote {
			ports = append(ports, f.Bind)
		}
	}
	if t.Type == model.TypeNgrok && t.Ngrok != nil {
		// ngrok and cloudflared connect to these rather than listen, but sharing one is still worth flagging.
		ports = append(ports, t.Ngrok.Addr)
	}
	return ports
}
