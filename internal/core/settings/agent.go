package settings

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

// Agent is the per-machine MCP policy. A nil Settings.Agent means defaults;
// a non-nil policy preserves an explicitly disabled Enabled value.
// Duration strings are kept in the same form as settings.toml.
type Agent struct {
	Enabled       bool     `toml:"enabled"`
	PortRange     string   `toml:"port_range"`
	DefaultTTL    string   `toml:"default_ttl"`
	MaxTTL        string   `toml:"max_ttl"`
	AllowTunnels  []string `toml:"allow_tunnels"`
	AllowSSHHosts []string `toml:"allow_ssh_hosts"`
	Public        string   `toml:"public"` // ask | deny; public exposure cannot be allow-listed
}

// DefaultAgent returns fresh allow-lists. The lists shown in the design are
// examples, not shipping grants: a new installation asks for every tunnel.
func DefaultAgent() Agent {
	return Agent{
		Enabled: true, PortRange: "20000-20999", DefaultTTL: "30m", MaxTTL: "8h",
		AllowTunnels: []string{}, AllowSSHHosts: []string{}, Public: "ask",
	}
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
	defaultTTL, err := time.ParseDuration(a.DefaultTTL)
	if err != nil || defaultTTL <= 0 {
		return fmt.Errorf("default_ttl must be a positive duration")
	}
	maxTTL, err := time.ParseDuration(a.MaxTTL)
	if err != nil || maxTTL <= 0 || maxTTL < defaultTTL {
		return fmt.Errorf("max_ttl must be a positive duration at least default_ttl")
	}
	if a.Public != "ask" && a.Public != "deny" {
		return fmt.Errorf("public must be ask or deny")
	}
	for _, pattern := range a.AllowTunnels {
		if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
			return fmt.Errorf("allow_tunnels contains an invalid glob")
		}
	}
	for _, alias := range a.AllowSSHHosts {
		if alias == "" || strings.HasPrefix(alias, "-") || strings.ContainsAny(alias, " \t\r\n\x00") {
			return fmt.Errorf("allow_ssh_hosts must contain literal SSH aliases")
		}
	}
	return nil
}
