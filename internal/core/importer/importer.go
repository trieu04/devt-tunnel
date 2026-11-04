// Package importer converts pasted tunnel commands into model drafts.
package importer

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

type Result struct {
	Tunnel   model.Tunnel      `json:"tunnel"`
	Warnings []string          `json:"warnings"`
	Secrets  []SecretCandidate `json:"secrets"`
}

type SecretCandidate struct {
	Suggested string `json:"suggested"`
	Value     string `json:"value"`
	Kind      string `json:"kind"`
}

func Parse(line string, d command.Dialect, knownHosts []string) (Result, error) {
	original := line
	line = strings.TrimSpace(line)
	if d == command.Windows {
		line = strings.ReplaceAll(line, "^\r\n", " ")
		line = strings.ReplaceAll(line, "^\n", " ")
	} else {
		line = strings.ReplaceAll(line, "\\\r\n", " ")
		line = strings.ReplaceAll(line, "\\\n", " ")
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "$ ") || strings.HasPrefix(line, "> ") {
		line = strings.TrimSpace(line[2:])
	}
	res := Result{Tunnel: model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: original}}}
	words, err := command.SplitArgs(line, d)
	if err != nil {
		return res, err
	}
	if len(words) == 0 {
		return res, fmt.Errorf("empty command")
	}
	if hasShellOperator(line, d) {
		res.Warnings = append(res.Warnings, "Shell operators are not parsed; command kept as-is")
		return res, nil
	}
	if eq := strings.IndexByte(words[0], '='); eq > 0 && !strings.HasPrefix(words[0], "-") {
		name, value := words[0][:eq], words[0][eq+1:]
		words = words[1:]
		switch name {
		case "NGROK_AUTHTOKEN":
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "ngrok-pasted", Value: value, Kind: "ngrok"})
		case "TUNNEL_TOKEN":
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "cloudflared-pasted", Value: value, Kind: "cloudflared"})
		default:
			res.Warnings = append(res.Warnings, "Environment assignment "+name+" is not applied")
		}
		if len(words) == 0 {
			return res, fmt.Errorf("environment assignment has no command")
		}
	}
	tool := strings.ToLower(filepath.Base(strings.Trim(words[0], `"'`)))
	if d == command.Windows {
		tool = strings.ToLower(filepath.Base(strings.ReplaceAll(strings.Trim(words[0], `"'`), `\`, `/`)))
	}
	tool = strings.TrimSuffix(tool, ".exe")
	args := words[1:]
	switch tool {
	case "ssh":
		return parseSSH(res, args, knownHosts, d)
	case "ngrok":
		return parseNgrok(res, args, d)
	case "cloudflared":
		return parseCloudflared(res, args, d)
	default:
		res.Tunnel.Command.Line = original
		return res, nil
	}
}

// sshArgs collects the parts of an ssh argv while it is scanned. Anything it cannot map
// onto a field goes to extra, so a round trip never drops an argument.
type sshArgs struct {
	res      *Result
	ssh      *model.SSH
	forwards []model.Forward
	extra    []string
	dest     string
}

const (
	// sshValueFlags take an argument; sshBoolFlags do not.
	sshValueFlags = "BbcDEeFIiJLlmOoPpQRSWw"
	sshBoolFlags  = "46AaCfGgKkMNnqsTtVvXxYy"
)

func parseSSH(res Result, args, knownHosts []string, d command.Dialect) (Result, error) {
	p := &sshArgs{res: &res, ssh: &model.SSH{Options: map[string]string{}}}
	if err := p.scan(args); err != nil {
		return res, err
	}
	if err := p.applyDestination(knownHosts); err != nil {
		return res, err
	}
	res.Tunnel = model.Tunnel{Type: model.TypeSSH, SSH: p.ssh, Forwards: p.forwards}
	p.ssh.ExtraArgs = joinArgs(p.extra, d)
	return res, nil
}

func (p *sshArgs) scan(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case !strings.HasPrefix(a, "-") || a == "-":
			if p.dest != "" {
				return fmt.Errorf("SSH remote commands cannot be represented; use a Command tunnel")
			}
			p.dest = a
		case a == "--":
			p.extra = append(p.extra, args[i:]...)
			return nil
		default:
			next, err := p.option(args, i)
			if err != nil {
				return err
			}
			i = next
		}
	}
	return nil
}

// option handles args[i], a single flag or a cluster such as -NCL, and returns the index of
// the last argument it consumed.
func (p *sshArgs) option(args []string, i int) (int, error) {
	cluster := args[i][1:]
	for j := 0; j < len(cluster); j++ {
		flag := rune(cluster[j])
		switch {
		case strings.ContainsRune(sshBoolFlags, flag):
			p.boolFlag(flag)
		case strings.ContainsRune(sshValueFlags, flag):
			value := cluster[j+1:]
			if value == "" {
				if i+1 >= len(args) {
					return i, fmt.Errorf("-%c requires a value", flag)
				}
				i++
				value = args[i]
				// "-o Key value" is the space-separated form of "-o Key=value".
				if flag == 'o' && !strings.Contains(value, "=") && i+1 < len(args) {
					i++
					value += " " + args[i]
				}
			}
			return i, p.value(flag, value)
		default:
			p.extra = append(p.extra, "-"+string(flag))
		}
	}
	return i, nil
}

func (p *sshArgs) boolFlag(flag rune) {
	switch flag {
	case 'N', 'C', 'T', 'A', 'q', 'v':
		p.ssh.Flags = append(p.ssh.Flags, "-"+string(flag))
	case 'f':
		p.res.Warnings = append(p.res.Warnings, "-f was dropped; devt-tunnel supervises the foreground process")
	default:
		p.extra = append(p.extra, "-"+string(flag))
	}
}

func (p *sshArgs) value(flag rune, value string) error {
	s := p.ssh
	switch flag {
	case 'p':
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid ssh port %q", value)
		}
		s.Port = n
	case 'i':
		s.Identity = value
	case 'l':
		s.User = value
	case 'J':
		s.Options["ProxyJump"] = value
	case 'o':
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			val = ""
			if strings.Contains(value, " ") {
				key, val, _ = strings.Cut(value, " ")
			}
		}
		if key == "" {
			return fmt.Errorf("invalid ssh -o option %q", value)
		}
		s.Options[key] = val
	case 'L', 'R', 'D':
		if strings.Contains(value, "/") {
			p.extra = append(p.extra, "-"+string(flag), value)
			return nil
		}
		f, err := parseForward(flag, value)
		if err != nil {
			return err
		}
		p.forwards = append(p.forwards, f)
	case 'F':
		p.extra = append(p.extra, "-F", value)
		p.res.Warnings = append(p.res.Warnings, "-F is kept as an argument; profiles normally select the SSH config")
	default:
		p.extra = append(p.extra, "-"+string(flag), value)
	}
	return nil
}

// applyDestination splits [ssh://][user@]host[:port] into the SSH fields.
func (p *sshArgs) applyDestination(knownHosts []string) error {
	s, dest := p.ssh, p.dest
	if dest == "" {
		return fmt.Errorf("ssh command has no destination")
	}
	if strings.Contains(dest, "://") {
		u, err := url.Parse(dest)
		if err == nil && u.Scheme == "ssh" {
			if u.User != nil {
				s.User = u.User.Username()
			}
			dest = u.Hostname()
			if u.Port() != "" {
				port, _ := strconv.Atoi(u.Port())
				s.Port = port
			}
		}
	}
	if at := strings.LastIndex(dest, "@"); at >= 0 {
		if s.User == "" {
			s.User = dest[:at]
		}
		dest = dest[at+1:]
	}
	if s.User != "" || s.Port != 0 || s.Identity != "" {
		s.Hostname = dest
	} else {
		s.Host = dest
	}
	if s.Host != "" && !contains(knownHosts, s.Host) {
		p.res.Warnings = append(p.res.Warnings, "SSH host "+s.Host+" was not found in the known hosts list")
	}
	return nil
}

func parseForward(k rune, value string) (model.Forward, error) {
	if k == 'D' {
		bind, port := splitBindPort(value)
		if port == "" {
			return model.Forward{}, fmt.Errorf("invalid -D forward %q", value)
		}
		return model.Forward{Kind: model.ForwardDynamic, Bind: bind + port}, nil
	}
	parts, err := splitColon(value)
	if err != nil || len(parts) < 3 {
		return model.Forward{}, fmt.Errorf("invalid -%c forward %q", k, value)
	}
	bind := "127.0.0.1:" + parts[0]
	targetHost, targetPort := parts[1], parts[2]
	if len(parts) >= 4 {
		bind = parts[0] + ":" + parts[1]
		targetHost, targetPort = parts[2], parts[3]
	}
	return model.Forward{Kind: model.ForwardKind(string(k)), Bind: bind, Target: targetHost + ":" + targetPort}, nil
}

func splitColon(s string) ([]string, error) {
	var out []string
	for len(s) > 0 {
		if s[0] == '[' {
			j := strings.IndexByte(s, ']')
			if j < 0 {
				return nil, fmt.Errorf("bad bracket")
			}
			if j+1 < len(s) && s[j+1] == ':' {
				out = append(out, s[:j+1])
				s = s[j+2:]
				continue
			}
		}
		i := strings.IndexByte(s, ':')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
	return out, nil
}
func splitBindPort(s string) (string, string) {
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]:"); i >= 0 {
			return s[:i+1] + ":", s[i+2:]
		}
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "127.0.0.1:", s
	}
	return s[:i+1], s[i+1:]
}

func parseNgrok(res Result, args []string, d command.Dialect) (Result, error) {
	if len(args) == 0 {
		return commandFallback(res, "ngrok config-file or start forms are not mapped"), nil
	}
	proto := args[0]
	if proto == "start" || proto == "config" {
		return commandFallback(res, "ngrok start/config forms are kept as a Command tunnel"), nil
	}
	if proto != "http" && proto != "tcp" && proto != "tls" {
		return commandFallback(res, "ngrok command form is kept as a Command tunnel"), nil
	}
	if len(args) < 2 {
		return res, fmt.Errorf("ngrok %s needs an address", proto)
	}
	addr := args[1]
	if _, err := strconv.Atoi(addr); err == nil {
		addr = "127.0.0.1:" + addr
	}
	n := &model.Ngrok{Proto: proto, Addr: addr}
	for _, candidate := range res.Secrets {
		if candidate.Kind == "ngrok" {
			n.Secret = candidate.Suggested
		}
	}
	var extra []string
	for i := 2; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--authtoken=") {
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "ngrok-pasted", Value: strings.TrimPrefix(a, "--authtoken="), Kind: "ngrok"})
			n.Secret = "ngrok-pasted"
			continue
		}
		if a == "--authtoken" {
			if i+1 >= len(args) {
				return res, fmt.Errorf("--authtoken requires a value")
			}
			i++
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "ngrok-pasted", Value: args[i], Kind: "ngrok"})
			n.Secret = "ngrok-pasted"
			continue
		}
		if a == "--log" || a == "--log-format" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(a, "--log=") || strings.HasPrefix(a, "--log-format=") {
			continue
		}
		if strings.HasPrefix(a, "--domain=") {
			n.Domain = strings.TrimPrefix(a, "--domain=")
			continue
		}
		if a == "--domain" || a == "--url" {
			if i+1 >= len(args) {
				return res, fmt.Errorf("%s requires a value", a)
			}
			i++
			if strings.HasPrefix(args[i], "https://") {
				u, _ := url.Parse(args[i])
				n.Domain = u.Host
			} else {
				n.Domain = args[i]
			}
			continue
		}
		extra = append(extra, a)
	}
	n.ExtraArgs = joinArgs(extra, d)
	res.Tunnel = model.Tunnel{Type: model.TypeNgrok, Ngrok: n}
	return res, nil
}

func parseCloudflared(res Result, args []string, d command.Dialect) (Result, error) {
	if len(args) == 0 {
		return res, fmt.Errorf("cloudflared command has no subcommand")
	}
	if args[0] == "service" || args[0] == "login" || args[0] == "access" {
		return res, fmt.Errorf("cloudflared %s is not a tunnel command", args[0])
	}
	if args[0] != "tunnel" {
		return commandFallback(res, "cloudflared command form is kept as a Command tunnel"), nil
	}
	c := &model.Cloudflared{}
	for _, candidate := range res.Secrets {
		if candidate.Kind == "cloudflared" {
			c.Secret = candidate.Suggested
		}
	}
	var extra []string
	run := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "run":
			run = true
		case a == "--url":
			if i+1 >= len(args) {
				return res, fmt.Errorf("--url requires a value")
			}
			i++
			c.URL = args[i]
		case strings.HasPrefix(a, "--url="):
			c.URL = strings.TrimPrefix(a, "--url=")
		case a == "--hostname":
			if i+1 >= len(args) {
				return res, fmt.Errorf("--hostname requires a value")
			}
			i++
			c.Hostname = args[i]
		case strings.HasPrefix(a, "--hostname="):
			c.Hostname = strings.TrimPrefix(a, "--hostname=")
		case a == "--token":
			if i+1 >= len(args) {
				return res, fmt.Errorf("--token requires a value")
			}
			i++
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "cloudflared-pasted", Value: args[i], Kind: "cloudflared"})
			c.Secret = "cloudflared-pasted"
		case strings.HasPrefix(a, "--token="):
			res.Secrets = append(res.Secrets, SecretCandidate{Suggested: "cloudflared-pasted", Value: strings.TrimPrefix(a, "--token="), Kind: "cloudflared"})
			c.Secret = "cloudflared-pasted"
		case strings.HasPrefix(a, "-"):
			extra = append(extra, a)
		default:
			if run && c.Name == "" {
				c.Name = a
			} else {
				extra = append(extra, a)
			}
		}
	}
	if c.URL == "" && c.Name == "" && len(res.Secrets) == 0 {
		return res, fmt.Errorf("cloudflared tunnel command needs --url or run")
	}
	c.ExtraArgs = joinArgs(extra, d)
	res.Tunnel = model.Tunnel{Type: model.TypeCloudflared, Cloudflared: c}
	return res, nil
}

func commandFallback(r Result, warning string) Result {
	r.Tunnel = model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: r.Tunnel.Command.Line}}
	r.Warnings = append(r.Warnings, warning)
	return r
}
func hasShellOperator(s string, d command.Dialect) bool {
	quoted, single, escaped := false, false, false
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		if escaped {
			escaped = false
			continue
		}
		if d == command.POSIX && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' && !single {
			quoted = !quoted
			continue
		}
		if d == command.POSIX && r == '\'' && !quoted {
			single = !single
			continue
		}
		if quoted || single {
			continue
		}
		if r == '|' || r == ';' || r == '>' {
			return true
		}
		if i+1 < len(s) && s[i:i+2] == "&&" {
			return true
		}
	}
	return false
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

func joinArgs(args []string, d command.Dialect) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg != "" && !strings.ContainsAny(arg, " \t\n\"'") {
			quoted[i] = arg
			continue
		}
		if d == command.Windows {
			quoted[i] = strconv.Quote(arg)
		} else {
			quoted[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg) + `"`
		}
	}
	return strings.Join(quoted, " ")
}
