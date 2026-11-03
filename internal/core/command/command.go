// Package command turns a tunnel definition into the exact process to spawn.
// The editor preview and the supervisor both use it, so they cannot diverge.
package command

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// Spec describes a process to spawn. Secret values are never part of it.
type Spec struct {
	Tool       string            // binary name, or the first shell word for command tunnels
	Args       []string          // arguments after Tool
	Dir        string            // working directory
	SecretEnv  map[string]string // environment variable → secret name
	Askpass    bool              // ssh: route prompts through the askpass helper
	Passphrase string            // ssh: secret name that answers passphrase prompts
	Ready      Ready
}

// Ready says how to detect that the tunnel is up. With nothing set, the process only has to stay alive.
type Ready struct {
	Ports      []string // local addresses that must accept connections
	LogPattern *regexp.Regexp
	AliveFor   time.Duration
}

// Options carries the values that depend on the environment.
type Options struct {
	SSHConfig string   // profile's ssh config, passed with -F
	Shell     []string // shell prefix for command tunnels, e.g. ["/bin/sh", "-c"]
}

const defaultAlive = 3 * time.Second

// Build returns the spawn spec for t.
func Build(t model.Tunnel, opts Options) (Spec, error) {
	switch t.Type {
	case model.TypeSSH:
		return buildSSH(t, opts)
	case model.TypeCloudflared:
		return buildCloudflared(t)
	case model.TypeNgrok:
		return buildNgrok(t)
	case model.TypeCommand:
		return buildCommand(t, opts)
	}
	return Spec{}, fmt.Errorf("unknown tunnel type %q", t.Type)
}

// Preview renders the command line for display, with POSIX quoting.
func (s Spec) Preview() string {
	parts := make([]string, 0, len(s.Args)+1)
	parts = append(parts, quote(s.Tool))
	for _, a := range s.Args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func buildSSH(t model.Tunnel, opts Options) (Spec, error) {
	c := t.SSH
	if c == nil {
		return Spec{}, fmt.Errorf("ssh settings missing")
	}
	var args []string
	if opts.SSHConfig != "" {
		args = append(args, "-F", opts.SSHConfig)
	}
	args = append(args, c.Flags...)

	// ExitOnForwardFailure makes a taken port an exit, not a silent half-working tunnel.
	// Disabling connection sharing keeps the forward in the process being supervised.
	options := map[string]string{
		"ExitOnForwardFailure": "yes",
		"ControlMaster":        "no",
		"ControlPath":          "none",
	}
	for k, v := range c.Options {
		options[k] = v
	}
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-o", k+"="+options[k])
	}

	var ready Ready
	for _, f := range t.Forwards {
		switch f.Kind {
		case model.ForwardLocal:
			args = append(args, "-L", f.Bind+":"+f.Target)
			ready.Ports = append(ready.Ports, f.Bind)
		case model.ForwardRemote:
			args = append(args, "-R", f.Bind+":"+f.Target)
		case model.ForwardDynamic:
			args = append(args, "-D", f.Bind)
			ready.Ports = append(ready.Ports, f.Bind)
		default:
			return Spec{}, fmt.Errorf("unknown forward kind %q", f.Kind)
		}
	}
	if len(ready.Ports) == 0 {
		ready.AliveFor = defaultAlive
	}

	extra, err := SplitArgs(c.ExtraArgs)
	if err != nil {
		return Spec{}, fmt.Errorf("extra_args: %w", err)
	}
	args = append(args, extra...)

	if c.Host != "" {
		args = append(args, c.Host)
	} else {
		if c.Port != 0 {
			args = append(args, "-p", strconv.Itoa(c.Port))
		}
		if c.Identity != "" {
			args = append(args, "-i", c.Identity)
		}
		dest := c.Hostname
		if c.User != "" {
			dest = c.User + "@" + dest
		}
		args = append(args, dest)
	}
	return Spec{Tool: "ssh", Args: args, Askpass: true, Passphrase: c.PassphraseSecret, Ready: ready}, nil
}

var (
	cloudflaredQuickReady = regexp.MustCompile(`https://[-a-z0-9.]+\.trycloudflare\.com`)
	cloudflaredNamedReady = regexp.MustCompile(`Registered tunnel connection`)
	ngrokReady            = regexp.MustCompile(`started tunnel.*url=`)
)

func buildCloudflared(t model.Tunnel) (Spec, error) {
	c := t.Cloudflared
	if c == nil {
		return Spec{}, fmt.Errorf("cloudflared settings missing")
	}
	if c.Name == "" && c.Secret == "" {
		return Spec{
			Tool:  "cloudflared",
			Args:  []string{"tunnel", "--url", c.URL},
			Ready: Ready{LogPattern: cloudflaredQuickReady},
		}, nil
	}
	args := []string{"tunnel"}
	if c.URL != "" {
		args = append(args, "--url", c.URL)
	}
	args = append(args, "run")
	if c.Name != "" {
		args = append(args, c.Name)
	}
	spec := Spec{Tool: "cloudflared", Args: args, Ready: Ready{LogPattern: cloudflaredNamedReady}}
	if c.Secret != "" {
		spec.SecretEnv = map[string]string{"TUNNEL_TOKEN": c.Secret}
	}
	return spec, nil
}

func buildNgrok(t model.Tunnel) (Spec, error) {
	c := t.Ngrok
	if c == nil {
		return Spec{}, fmt.Errorf("ngrok settings missing")
	}
	// Without --log ngrok draws a terminal UI and readiness cannot be read from its output.
	args := []string{c.Proto, c.Addr, "--log=stdout", "--log-format=logfmt"}
	if c.Domain != "" {
		args = append(args, "--domain="+c.Domain)
	}
	spec := Spec{Tool: "ngrok", Args: args, Ready: Ready{LogPattern: ngrokReady}}
	if c.Secret != "" {
		spec.SecretEnv = map[string]string{"NGROK_AUTHTOKEN": c.Secret}
	}
	return spec, nil
}

func buildCommand(t model.Tunnel, opts Options) (Spec, error) {
	c := t.Command
	if c == nil {
		return Spec{}, fmt.Errorf("command settings missing")
	}
	if len(opts.Shell) == 0 {
		return Spec{}, fmt.Errorf("no shell configured")
	}
	spec := Spec{
		Tool:      opts.Shell[0],
		Args:      append(slices.Clone(opts.Shell[1:]), c.Line),
		Dir:       c.Dir,
		SecretEnv: c.SecretEnvs,
		Ready:     Ready{AliveFor: defaultAlive},
	}
	if c.ReadyLog != "" {
		re, err := regexp.Compile(c.ReadyLog)
		if err != nil {
			return Spec{}, fmt.Errorf("ready_log: %w", err)
		}
		spec.Ready = Ready{LogPattern: re}
	}
	return spec, nil
}

// SplitArgs splits a string into arguments using POSIX shell quoting rules.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quoteCh rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quoteCh == '\'':
			if r == '\'' {
				quoteCh = 0
			} else {
				cur.WriteRune(r)
			}
		case quoteCh == '"':
			switch r {
			case '"':
				quoteCh = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quoteCh = r
			inArg = true
		case r == '\\':
			escaped = true
			inArg = true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quoteCh != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quoteCh)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

func quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]#~!{}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
