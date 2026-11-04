// Package model defines tunnels and profiles as stored in tunnels.toml.
package model

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type TunnelType string

const (
	TypeSSH         TunnelType = "ssh"
	TypeCloudflared TunnelType = "cloudflared"
	TypeNgrok       TunnelType = "ngrok"
	TypeCommand     TunnelType = "command"
)

type ForwardKind string

const (
	ForwardLocal   ForwardKind = "L"
	ForwardRemote  ForwardKind = "R"
	ForwardDynamic ForwardKind = "D"
)

// Config is the content of tunnels.toml.
type Config struct {
	Profiles []Profile `toml:"profile" json:"profiles"`
	Tunnels  []Tunnel  `toml:"tunnel" json:"tunnels"`
}

type Profile struct {
	ID             string   `toml:"id" json:"id"`
	Name           string   `toml:"name" json:"name"`
	Description    string   `toml:"description,omitempty" json:"description,omitempty"`
	SSHConfig      string   `toml:"ssh_config,omitempty" json:"sshConfig,omitempty"`
	OnActivate     string   `toml:"on_activate,omitempty" json:"onActivate,omitempty"`
	StopNonMembers bool     `toml:"stop_non_members,omitempty" json:"stopNonMembers,omitempty"`
	Autostart      []string `toml:"autostart,omitempty" json:"autostart,omitempty"`
}

type Tunnel struct {
	ID            string     `toml:"id" json:"id"`
	Name          string     `toml:"name" json:"name"`
	Type          TunnelType `toml:"type" json:"type"`
	Profiles      []string   `toml:"profiles,omitempty" json:"profiles,omitempty"`
	Favorite      bool       `toml:"favorite,omitempty" json:"favorite,omitempty"`
	StartOnLaunch bool       `toml:"start_on_launch,omitempty" json:"startOnLaunch,omitempty"`
	Reconnect     bool       `toml:"reconnect,omitempty" json:"reconnect,omitempty"`

	SSH         *SSH         `toml:"ssh,omitempty" json:"ssh,omitempty"`
	Cloudflared *Cloudflared `toml:"cloudflared,omitempty" json:"cloudflared,omitempty"`
	Ngrok       *Ngrok       `toml:"ngrok,omitempty" json:"ngrok,omitempty"`
	Command     *Command     `toml:"command,omitempty" json:"command,omitempty"`
	Forwards    []Forward    `toml:"forward,omitempty" json:"forwards,omitempty"`
}

// SSH holds either a config alias in Host, or manual User, Hostname, Port and Identity.
type SSH struct {
	Host             string            `toml:"host,omitempty" json:"host,omitempty"`
	User             string            `toml:"user,omitempty" json:"user,omitempty"`
	Hostname         string            `toml:"hostname,omitempty" json:"hostname,omitempty"`
	Port             int               `toml:"port,omitempty" json:"port,omitempty"`
	Identity         string            `toml:"identity,omitempty" json:"identity,omitempty"`
	Flags            []string          `toml:"flags,omitempty" json:"flags,omitempty"`
	Options          map[string]string `toml:"options,omitempty" json:"options,omitempty"`
	ExtraArgs        string            `toml:"extra_args,omitempty" json:"extraArgs,omitempty"`
	PassphraseSecret string            `toml:"passphrase_secret,omitempty" json:"passphraseSecret,omitempty"`
}

// Forward is one SSH port mapping. Bind listens and Target receives; D has no Target.
type Forward struct {
	Kind   ForwardKind `toml:"kind" json:"kind"`
	Bind   string      `toml:"bind" json:"bind"`
	Target string      `toml:"target,omitempty" json:"target,omitempty"`
}

// Cloudflared runs a named tunnel when Name or Secret is set, otherwise a quick tunnel of URL.
type Cloudflared struct {
	Name      string `toml:"name,omitempty" json:"name,omitempty"`
	Hostname  string `toml:"hostname,omitempty" json:"hostname,omitempty"`
	URL       string `toml:"url,omitempty" json:"url,omitempty"`
	Secret    string `toml:"secret,omitempty" json:"secret,omitempty"`
	ExtraArgs string `toml:"extra_args,omitempty" json:"extraArgs,omitempty"`
}

type Ngrok struct {
	Proto     string `toml:"proto" json:"proto"`
	Addr      string `toml:"addr" json:"addr"`
	Domain    string `toml:"domain,omitempty" json:"domain,omitempty"`
	Secret    string `toml:"secret,omitempty" json:"secret,omitempty"`
	ExtraArgs string `toml:"extra_args,omitempty" json:"extraArgs,omitempty"`
}

type Command struct {
	Line       string            `toml:"line" json:"line"`
	Dir        string            `toml:"dir,omitempty" json:"dir,omitempty"`
	ReadyLog   string            `toml:"ready_log,omitempty" json:"readyLog,omitempty"`
	SecretEnvs map[string]string `toml:"secret_envs,omitempty" json:"secretEnvs,omitempty"`
}

// Validate reports the first structural problem in the config.
func (c Config) Validate() error {
	profiles := map[string]bool{}
	for _, p := range c.Profiles {
		if p.ID == "" {
			return fmt.Errorf("profile %q has no id", p.Name)
		}
		if profiles[p.ID] {
			return fmt.Errorf("duplicate profile id %q", p.ID)
		}
		profiles[p.ID] = true
		switch p.OnActivate {
		case "", "filter", "start_autostart":
		default:
			return fmt.Errorf("profile %q has invalid on_activate %q", p.ID, p.OnActivate)
		}
	}
	tunnels := map[string]bool{}
	for _, t := range c.Tunnels {
		if t.ID == "" {
			return fmt.Errorf("tunnel %q has no id", t.Name)
		}
		if tunnels[t.ID] {
			return fmt.Errorf("duplicate tunnel id %q", t.ID)
		}
		tunnels[t.ID] = true
		for _, p := range t.Profiles {
			if !profiles[p] {
				return fmt.Errorf("tunnel %q uses unknown profile %q", t.ID, p)
			}
		}
		if err := t.validateSettings(); err != nil {
			return fmt.Errorf("tunnel %q: %w", t.ID, err)
		}
	}
	for _, p := range c.Profiles {
		for _, id := range p.Autostart {
			found := false
			for _, t := range c.Tunnels {
				if t.ID == id && contains(t.Profiles, p.ID) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("profile %q autostart tunnel %q is not a member", p.ID, id)
			}
		}
	}
	return nil
}

func (t Tunnel) validateSettings() error {
	switch t.Type {
	case TypeSSH:
		if t.SSH == nil || (t.SSH.Host == "" && t.SSH.Hostname == "") {
			return fmt.Errorf("ssh needs host or hostname")
		}
		for _, f := range t.Forwards {
			if err := validateAddress(f.Bind); err != nil {
				return fmt.Errorf("forward bind %q: %w", f.Bind, err)
			}
			switch f.Kind {
			case ForwardLocal, ForwardRemote:
				if f.Target == "" {
					return fmt.Errorf("-%s forward %q needs a target", f.Kind, f.Bind)
				}
				if err := validateAddress(f.Target); err != nil {
					return fmt.Errorf("forward target %q: %w", f.Target, err)
				}
			case ForwardDynamic:
			default:
				return fmt.Errorf("unknown forward kind %q", f.Kind)
			}
		}
	case TypeCloudflared:
		if c := t.Cloudflared; c == nil || (c.Name == "" && c.Secret == "" && c.URL == "") {
			return fmt.Errorf("cloudflared needs a name, secret or url")
		}
	case TypeNgrok:
		if t.Ngrok == nil || t.Ngrok.Proto == "" || t.Ngrok.Addr == "" {
			return fmt.Errorf("ngrok needs proto and addr")
		}
		switch t.Ngrok.Proto {
		case "http", "tcp", "tls":
		default:
			return fmt.Errorf("ngrok proto must be http, tcp or tls")
		}
	case TypeCommand:
		if t.Command == nil || t.Command.Line == "" {
			return fmt.Errorf("command needs a line")
		}
		if t.Command.ReadyLog != "" {
			if _, err := regexp.Compile(t.Command.ReadyLog); err != nil {
				return fmt.Errorf("command ready_log: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown type %q", t.Type)
	}
	return nil
}

func validateAddress(value string) error {
	if strings.Contains(value, "/") {
		return nil
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		port = value
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// NewID returns a stable slug, adding a numeric suffix when it is already taken.
func NewID(name string, taken map[string]bool) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "tunnel"
	}
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// RemoveTunnelReferences removes an id from profile autostart lists.
func RemoveTunnelReferences(c *Config, id string) {
	for i := range c.Profiles {
		out := c.Profiles[i].Autostart[:0]
		for _, candidate := range c.Profiles[i].Autostart {
			if candidate != id {
				out = append(out, candidate)
			}
		}
		c.Profiles[i].Autostart = out
	}
}

// Secrets lists the secret names the tunnel needs before it can start.
func (t Tunnel) Secrets() []string {
	var names []string
	switch {
	case t.Type == TypeSSH && t.SSH != nil && t.SSH.PassphraseSecret != "":
		names = append(names, t.SSH.PassphraseSecret)
	case t.Type == TypeCloudflared && t.Cloudflared != nil && t.Cloudflared.Secret != "":
		names = append(names, t.Cloudflared.Secret)
	case t.Type == TypeNgrok && t.Ngrok != nil && t.Ngrok.Secret != "":
		names = append(names, t.Ngrok.Secret)
	case t.Type == TypeCommand && t.Command != nil:
		for _, s := range t.Command.SecretEnvs {
			names = append(names, s)
		}
	}
	return names
}
