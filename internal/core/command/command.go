// Package command turns a tunnel definition into the exact process to spawn.
// The editor preview and the supervisor both use it, so they cannot diverge.
package command

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
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
	Tool        string            // binary name, or the first shell word for command tunnels
	Args        []string          // arguments after Tool
	Dir         string            // working directory
	SecretEnv   map[string]string // environment variable → secret name
	Askpass     bool              // ssh: route prompts through the askpass helper
	Passphrase  string            // ssh: secret name that answers passphrase prompts
	Ready       Ready
	CommandLine string // original command tunnel line, used for an exact Windows preview
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
	Home      string
	Dialect   Dialect
}

type Dialect int

const (
	POSIX Dialect = iota
	Windows
)

const defaultAlive = 3 * time.Second

// Build returns the spawn spec for t.
func Build(t model.Tunnel, opts Options) (Spec, error) {
	switch t.Type {
	case model.TypeSSH:
		return buildSSH(t, opts)
	case model.TypeCloudflared:
		return buildCloudflared(t, opts)
	case model.TypeNgrok:
		return buildNgrok(t, opts)
	case model.TypeCommand:
		return buildCommand(t, opts)
	}
	return Spec{}, fmt.Errorf("unknown tunnel type %q", t.Type)
}

// Preview renders the command line for display using the platform's quoting rules.
func (s Spec) Preview(d Dialect) string {
	if d == Windows && s.CommandLine != "" {
		return s.CommandLine
	}
	parts := make([]string, 0, len(s.Args)+1)
	parts = append(parts, quote(s.Tool, d))
	for _, a := range s.Args {
		parts = append(parts, quote(a, d))
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
		args = append(args, "-F", expandHome(opts.SSHConfig, opts.Home))
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

	extra, err := SplitArgs(c.ExtraArgs, opts.Dialect)
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

func buildCloudflared(t model.Tunnel, opts Options) (Spec, error) {
	c := t.Cloudflared
	if c == nil {
		return Spec{}, fmt.Errorf("cloudflared settings missing")
	}
	if c.Name == "" && c.Secret == "" {
		extra, err := SplitArgs(c.ExtraArgs, opts.Dialect)
		if err != nil {
			return Spec{}, fmt.Errorf("extra_args: %w", err)
		}
		return Spec{
			Tool:  "cloudflared",
			Args:  append([]string{"tunnel", "--url", c.URL}, extra...),
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
	extra, err := SplitArgs(c.ExtraArgs, opts.Dialect)
	if err != nil {
		return Spec{}, fmt.Errorf("extra_args: %w", err)
	}
	args = append(args, extra...)
	spec := Spec{Tool: "cloudflared", Args: args, Ready: Ready{LogPattern: cloudflaredNamedReady}}
	if c.Secret != "" {
		spec.SecretEnv = map[string]string{"TUNNEL_TOKEN": c.Secret}
	}
	return spec, nil
}

func buildNgrok(t model.Tunnel, opts Options) (Spec, error) {
	c := t.Ngrok
	if c == nil {
		return Spec{}, fmt.Errorf("ngrok settings missing")
	}
	// Without --log ngrok draws a terminal UI and readiness cannot be read from its output.
	args := []string{c.Proto, c.Addr, "--log=stdout", "--log-format=logfmt"}
	if c.Domain != "" {
		args = append(args, "--domain="+c.Domain)
	}
	extra, err := SplitArgs(c.ExtraArgs, opts.Dialect)
	if err != nil {
		return Spec{}, fmt.Errorf("extra_args: %w", err)
	}
	args = append(args, extra...)
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
		Tool:        opts.Shell[0],
		Args:        append(slices.Clone(opts.Shell[1:]), c.Line),
		Dir:         expandHome(c.Dir, opts.Home),
		SecretEnv:   c.SecretEnvs,
		Ready:       Ready{AliveFor: defaultAlive},
		CommandLine: c.Line,
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

// SplitArgs splits a string according to the selected command-line dialect.
func SplitArgs(s string, d Dialect) ([]string, error) {
	if d == Windows {
		return splitWindows(s)
	}
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

func quote(s string, d Dialect) string {
	if d == Windows {
		return quoteWindows(s)
	}
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?[]#~!{}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func splitWindows(s string) ([]string, error) {
	var args []string
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			break
		}
		var b strings.Builder
		quoted := false
		for i < len(s) {
			if !quoted && (s[i] == ' ' || s[i] == '\t') {
				break
			}
			if s[i] == '"' {
				quoted = !quoted
				i++
				continue
			}
			if s[i] == '\\' {
				start := i
				for i < len(s) && s[i] == '\\' {
					i++
				}
				n := i - start
				if i < len(s) && s[i] == '"' {
					b.WriteString(strings.Repeat("\\", n/2))
					if n%2 == 1 {
						b.WriteByte('"')
						i++
					} else {
						quoted = !quoted
						i++
					}
				} else {
					b.WriteString(strings.Repeat("\\", n))
				}
				continue
			}
			b.WriteByte(s[i])
			i++
		}
		if quoted {
			return nil, fmt.Errorf("unterminated double quote")
		}
		args = append(args, b.String())
	}
	return args, nil
}

func quoteWindows(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range s {
		if r == '\\' {
			backslashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat("\\", backslashes*2+1))
			b.WriteRune(r)
		} else {
			b.WriteString(strings.Repeat("\\", backslashes))
			b.WriteRune(r)
		}
		backslashes = 0
	}
	b.WriteString(strings.Repeat("\\", backslashes*2))
	b.WriteByte('"')
	return b.String()
}

func expandHome(path, home string) string {
	if home == "" || path == "~" {
		if path == "~" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return home + path[1:]
	}
	return path
}

// DialAddress normalizes the bind forms accepted by ssh for local TCP probes.
func DialAddress(bind string) string {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return net.JoinHostPort("127.0.0.1", bind)
	}
	if host == "" || host == "*" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// Fingerprint is the SHA-256 identity of argv (Tool and ordered Args), Dir,
// Askpass, Passphrase's secret name, and sorted (environment name, secret name)
// pairs. Spec never contains secret values, so rotating a secret does not
// invalidate an approval. Readiness and preview-only metadata are excluded.
func (s Spec) Fingerprint() string {
	keys := make([]string, 0, len(s.SecretEnv))
	for env := range s.SecretEnv {
		keys = append(keys, env)
	}
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "%q\x00%q\x00%q\x00%t\x00%q\x00", s.Tool, s.Args, s.Dir, s.Askpass, s.Passphrase)
	for _, env := range keys {
		// Frame both strings separately: concatenating env+"="+name would
		// collide for e.g. ("TOKEN=A", "B") and ("TOKEN", "A=B").
		fmt.Fprintf(h, "%q\x00%q\x00", env, s.SecretEnv[env])
	}
	return hex.EncodeToString(h.Sum(nil))
}
