package settings

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Agent is the per-machine MCP policy. A nil Settings.Agent means defaults;
// a non-nil policy preserves an explicitly disabled Enabled value.
// Duration strings are kept in the same form as settings.toml.
type Agent struct {
	Enabled       bool     `toml:"enabled" json:"enabled"`
	PortRange     string   `toml:"port_range" json:"portRange"`
	AllowTunnels  []string `toml:"allow_tunnels" json:"allowTunnels"`
	AllowSSHHosts []string `toml:"allow_ssh_hosts" json:"allowSSHHosts"`
	Public        string   `toml:"public" json:"public"` // ask | deny | allow; ask/deny cannot be allow-listed, allow is an explicit opt-in
}

// DefaultAgent returns fresh allow-lists. A new installation trusts what the
// person already set up: every saved tunnel and every SSH config alias. Public
// exposure, unsafe saved tunnels, manual hosts, secrets and saves still ask.
// Set the lists to [] to ask for everything.
func DefaultAgent() Agent {
	return Agent{
		Enabled: true, PortRange: "20000-20999",
		AllowTunnels: []string{"*"}, AllowSSHHosts: []string{"*"}, Public: "ask",
	}
}

// StrictAgent is DefaultAgent with empty allow-lists: every agent request that
// is not read-only asks a person.
func StrictAgent() Agent {
	a := DefaultAgent()
	a.AllowTunnels, a.AllowSSHHosts = []string{}, []string{}
	return a
}

// AgentPolicy also makes an in-memory zero Settings safe to use without Load.
func (s Settings) AgentPolicy() Agent {
	if s.Agent == nil {
		return DefaultAgent()
	}
	return *s.Agent
}

// Ports returns the inclusive allocation range without consulting the OS.
func (a Agent) Ports() (first, last int, err error) {
	parts := strings.Split(a.PortRange, "-")
	if len(parts) == 2 {
		first, err = strconv.Atoi(parts[0])
		if err == nil {
			last, err = strconv.Atoi(parts[1])
		}
		if err == nil && first > 0 && first <= last && last <= 65535 &&
			strconv.Itoa(first) == parts[0] && strconv.Itoa(last) == parts[1] {
			return first, last, nil
		}
	}
	return 0, 0, fmt.Errorf("port_range must be an inclusive range between 1 and 65535")
}

// Validate rejects invalid policy rather than silently broadening permissions.
func (a Agent) Validate() error {
	if _, _, err := a.Ports(); err != nil {
		return err
	}
	if a.Public != "ask" && a.Public != "deny" && a.Public != "allow" {
		return fmt.Errorf("public must be ask, deny or allow")
	}
	for _, pattern := range a.AllowTunnels {
		if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
			return fmt.Errorf("allow_tunnels contains an invalid glob")
		}
	}
	for _, alias := range a.AllowSSHHosts {
		if alias == "*" {
			continue
		}
		if alias == "" || strings.HasPrefix(alias, "-") || strings.ContainsAny(alias, " \t\r\n\x00") {
			return fmt.Errorf("allow_ssh_hosts must contain literal SSH aliases")
		}
	}
	return nil
}
