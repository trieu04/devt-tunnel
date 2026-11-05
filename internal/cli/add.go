package cli

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(addCommand) }

func addCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add",
		Short:   "Define and save a supervised tunnel",
		Long:    "Define an ssh, ngrok, cloudflared or command tunnel using the same defaults and evaluation as the app editor. --dry-run prints the exact command that would run and never saves or starts it. Secret flags accept existing keychain names only, never credential values.",
		Example: "  dtt add ssh db --host server1 -L 15432:5432\n  dtt add cloudflared web --url http://localhost:3000 --dry-run",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(addSSHCommand(a), addNgrokCommand(a), addCloudflaredCommand(a), addShellCommand(a))
	return cmd
}

func addSSHCommand(a *app) *cobra.Command {
	var options defineOptions
	var host, hostname, user, identity string
	var port int
	var local, remote, dynamic, flags, sshOptions []string
	cmd := &cobra.Command{
		Use:     "ssh <name> --host H -L M",
		Short:   "Save an SSH tunnel with local, remote or SOCKS forwards",
		Long:    "Save the equivalent of ssh -N -L 127.0.0.1:15432:127.0.0.1:5432 server1. Use --host for an SSH config alias, or --hostname with --user, --port and --identity for a manual destination. Repeat -L, -R, -D, --flag and --option. Maps accept port:port, port:host:port or bind:port:host:port; non-loopback binds warn. Saved definitions require concrete listen ports; use dtt forward for auto allocation.",
		Example: "  dtt add ssh db --host server1 -L 15432:5432\n  dtt add ssh proxy --hostname example.com --user dev --port 22 --identity ~/.ssh/id_ed25519 -D 1080\n  dtt add ssh web --host server1 -L 8080:app:80 --flag C --option ServerAliveInterval=15 --dry-run",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (host == "") == (hostname == "") {
				return usageError("provide exactly one of --host or --hostname")
			}
			if host != "" && (user != "" || cmd.Flags().Changed("port") || identity != "") {
				return usageError("--user, --port and --identity require --hostname, not --host")
			}
			if cmd.Flags().Changed("port") && (port < 1 || port > 65535) {
				return usageError("--port must be between 1 and 65535")
			}
			var forwards []model.Forward
			var warnings []string
			for _, group := range []struct {
				kind model.ForwardKind
				maps []string
			}{{model.ForwardLocal, local}, {model.ForwardRemote, remote}, {model.ForwardDynamic, dynamic}} {
				for _, value := range group.maps {
					forward, warning, err := parsePortMap(group.kind, value)
					if err != nil {
						return err
					}
					if forward.ListenPort != "" {
						return usageError("saved definitions require concrete ports; use dtt forward HOST auto:PORT --save NAME")
					}
					forwards = append(forwards, forward)
					if warning != "" {
						warnings = append(warnings, warning)
					}
				}
			}
			var extraFlags []string
			for _, value := range flags {
				value = strings.TrimPrefix(value, "-")
				if value == "" || strings.Trim(value, "46AaCfGgKkMNnqsTtVvXxYy") != "" {
					return usageError("--flag accepts SSH boolean flags only (for example N, C or -v); use --option KEY=VALUE for options")
				}
				for _, flag := range value {
					extraFlags = append(extraFlags, "-"+string(flag))
				}
			}
			parsedOptions := map[string]string{}
			for _, value := range sshOptions {
				key, val, ok := strings.Cut(value, "=")
				if !ok || key == "" || strings.ContainsFunc(key, unicode.IsSpace) {
					return usageError("--option must be KEY=VALUE")
				}
				parsedOptions[key] = val
			}
			t, err := a.newSSHDraft(cmd.Context(), args[0], forwards)
			if err != nil {
				return err
			}
			t.SSH.Host, t.SSH.Hostname = host, hostname
			t.SSH.User, t.SSH.Port, t.SSH.Identity = user, port, identity
			for _, flag := range extraFlags {
				found := false
				for _, existing := range t.SSH.Flags {
					found = found || flag == existing
				}
				if !found || flag != "-N" {
					t.SSH.Flags = append(t.SSH.Flags, flag)
				}
			}
			if t.SSH.Options == nil {
				t.SSH.Options = map[string]string{}
			}
			for key, val := range parsedOptions {
				t.SSH.Options[key] = val
			}
			if err := options.apply(cmd.Context(), a, &t); err != nil {
				return err
			}
			return a.finishDefinition(cmd.Context(), t, options, warnings)
		},
	}
	f := cmd.Flags()
	f.StringVar(&host, "host", "", "SSH config host alias (alternative to --hostname)")
	f.StringVar(&hostname, "hostname", "", "Manual SSH hostname (alternative to --host)")
	f.StringVar(&user, "user", "", "SSH user for --hostname")
	f.IntVar(&port, "port", 0, "SSH port for --hostname (default: SSH's port)")
	f.StringVar(&identity, "identity", "", "Identity file for --hostname")
	f.StringArrayVarP(&local, "local", "L", nil, "Local forward port:port, port:host:port or bind:port:host:port (repeatable)")
	f.StringArrayVarP(&remote, "remote", "R", nil, "Remote forward port:port, port:host:port or bind:port:host:port (repeatable)")
	f.StringArrayVarP(&dynamic, "socks", "D", nil, "SOCKS listen port or bind:port (repeatable)")
	f.StringArrayVar(&flags, "flag", nil, "Additional SSH boolean flag, e.g. C or -v (repeatable)")
	f.StringArrayVar(&sshOptions, "option", nil, "SSH option KEY=VALUE (repeatable)")
	options.flags(cmd)
	return cmd
}

func addNgrokCommand(a *app) *cobra.Command {
	var options defineOptions
	var proto, addr, domain, secretName string
	cmd := &cobra.Command{
		Use:     "ngrok <name> --proto http --addr A",
		Short:   "Save a public ngrok tunnel",
		Long:    "Save the equivalent of ngrok http http://127.0.0.1:3000, with log output configured for supervision. --secret is an existing keychain secret NAME, never an authtoken value. The tunnel will be public when started.",
		Example: "  dtt add ngrok web --proto http --addr http://localhost:3000 --secret ngrok-work\n  dtt add ngrok tcp-db --proto tcp --addr localhost:5432 --dry-run",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if addr == "" {
				return usageError("--addr is required")
			}
			var t model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: model.TypeNgrok}, &t); err != nil {
				return err
			}
			t.Name = args[0]
			t.Ngrok.Proto, t.Ngrok.Addr, t.Ngrok.Domain, t.Ngrok.Secret = proto, addr, domain, secretName
			if err := options.apply(cmd.Context(), a, &t); err != nil {
				return err
			}
			return a.finishDefinition(cmd.Context(), t, options, nil)
		},
	}
	f := cmd.Flags()
	f.StringVar(&proto, "proto", "http", "ngrok protocol: http, tcp or tls")
	f.StringVar(&addr, "addr", "", "Local service address (required)")
	f.StringVar(&domain, "domain", "", "Public ngrok domain")
	f.StringVar(&secretName, "secret", "", "Existing keychain secret NAME for ngrok authentication; never a secret value")
	options.flags(cmd)
	return cmd
}

func addCloudflaredCommand(a *app) *cobra.Command {
	var options defineOptions
	var url, tunnelName, hostname, secretName string
	cmd := &cobra.Command{
		Use:     "cloudflared <name> --url U",
		Short:   "Save a public quick or named Cloudflare Tunnel",
		Long:    "Save the equivalent of cloudflared tunnel --url http://localhost:3000 or cloudflared tunnel run NAME. Use --url for a quick tunnel, or --tunnel-name with optional --hostname and --secret for a named tunnel. --secret is an existing keychain secret NAME, never a token value. The tunnel will be public when started.",
		Example: "  dtt add cloudflared web --url http://localhost:3000\n  dtt add cloudflared work --tunnel-name work --hostname work.example.com --secret cloudflare-work --dry-run",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (url == "") == (tunnelName == "") {
				return usageError("provide exactly one of --url or --tunnel-name")
			}
			if url != "" && (hostname != "" || secretName != "") {
				return usageError("--hostname and --secret require --tunnel-name, not a quick --url tunnel")
			}
			var t model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: model.TypeCloudflared}, &t); err != nil {
				return err
			}
			t.Name = args[0]
			// Clear the quick URL default for named tunnels.
			t.Cloudflared.URL, t.Cloudflared.Name, t.Cloudflared.Hostname, t.Cloudflared.Secret = url, tunnelName, hostname, secretName
			if err := options.apply(cmd.Context(), a, &t); err != nil {
				return err
			}
			return a.finishDefinition(cmd.Context(), t, options, nil)
		},
	}
	f := cmd.Flags()
	f.StringVar(&url, "url", "", "Local service URL for a quick tunnel (alternative to --tunnel-name)")
	f.StringVar(&tunnelName, "tunnel-name", "", "Existing named Cloudflare tunnel (alternative to --url)")
	f.StringVar(&hostname, "hostname", "", "Public hostname metadata for a named tunnel (routing is managed by Cloudflare)")
	f.StringVar(&secretName, "secret", "", "Existing keychain secret NAME for the named tunnel; never a secret value")
	options.flags(cmd)
	return cmd
}

func addShellCommand(a *app) *cobra.Command {
	var options defineOptions
	var line, dir, readyLog string
	var secretEnvs []string
	cmd := &cobra.Command{
		Use:     "command <name> --line L",
		Short:   "Save an arbitrary supervised shell command",
		Long:    "Save a shell command as a tunnel, equivalent to running sh -c LINE (or the configured platform shell). Optionally set its working directory and readiness log regular expression. --secret-env maps ENV=NAME to an existing keychain reference; NAME is never a secret value and values never enter the preview command.",
		Example: "  dtt add command proxy --line 'my-proxy --port 8080' --ready-log 'Listening'\n  dtt add command api --line './start-api' --dir ~/work/api --secret-env API_KEY=api-work --dry-run",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(line) == "" {
				return usageError("--line is required and must not be empty")
			}
			refs := map[string]string{}
			for _, pair := range secretEnvs {
				key, name, ok := strings.Cut(pair, "=")
				if !ok || !defineEnvName(key) || strings.TrimSpace(name) == "" {
					return usageError("--secret-env must be ENV=NAME, where NAME is an existing keychain secret name, never a value")
				}
				refs[key] = name
			}
			var t model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: model.TypeCommand}, &t); err != nil {
				return err
			}
			t.Name = args[0]
			t.Command.Line, t.Command.Dir, t.Command.ReadyLog, t.Command.SecretEnvs = line, dir, readyLog, refs
			if err := options.apply(cmd.Context(), a, &t); err != nil {
				return err
			}
			return a.finishDefinition(cmd.Context(), t, options, nil)
		},
	}
	f := cmd.Flags()
	f.StringVar(&line, "line", "", "Shell command line (required)")
	f.StringVar(&dir, "dir", "", "Command working directory")
	f.StringVar(&readyLog, "ready-log", "", "Regular expression that marks readiness when matched in the log")
	f.StringArrayVar(&secretEnvs, "secret-env", nil, "ENV=NAME mapping to an existing keychain secret name; never a secret value (repeatable)")
	options.flags(cmd)
	return cmd
}

func defineEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (a *app) newSSHDraft(ctx context.Context, name string, forwards []model.Forward) (model.Tunnel, error) {
	var t model.Tunnel
	err := a.call(ctx, "tunnel.newDraft", api.NewDraftParams{Kind: model.TypeSSH}, &t)
	t.Name, t.Forwards = name, forwards
	return t, err
}

// parsePortMap expands CLI shorthand into the model's explicit bind and target.
// IPv6 addresses must be bracketed, just as in an ssh port mapping.
func parsePortMap(kind model.ForwardKind, value string) (model.Forward, string, error) {
	bad := func(message string) (model.Forward, string, error) {
		return model.Forward{}, "", usageError("invalid -%s map %q: %s", kind, value, message)
	}
	parts, err := defineColonParts(value)
	if err != nil {
		return bad(err.Error())
	}
	bindHost, targetHost := "127.0.0.1", "127.0.0.1"
	var bindPort, targetPort string
	if kind == model.ForwardDynamic {
		switch len(parts) {
		case 1:
			bindPort = parts[0]
		case 2:
			bindHost, bindPort = parts[0], parts[1]
		default:
			return bad("expected port or bind:port")
		}
	} else if kind == model.ForwardLocal || kind == model.ForwardRemote {
		switch len(parts) {
		case 2:
			bindPort, targetPort = parts[0], parts[1]
		case 3:
			bindPort, targetHost, targetPort = parts[0], parts[1], parts[2]
		case 4:
			bindHost, bindPort, targetHost, targetPort = parts[0], parts[1], parts[2], parts[3]
		default:
			return bad("expected port:port, port:host:port or bind:port:host:port")
		}
	} else {
		return bad("unknown forward kind")
	}
	auto := bindPort == "auto"
	if auto && (kind == model.ForwardRemote || bindHost != "127.0.0.1") {
		return bad("auto requires a loopback local or SOCKS forward")
	}
	if (!auto && !defineValidPort(bindPort)) || kind != model.ForwardDynamic && !defineValidPort(targetPort) {
		return bad("ports must be between 1 and 65535")
	}
	if targetHost == "" || strings.ContainsAny(bindHost+targetHost, " \t\r\n/") {
		return bad("host must be a hostname or IP address")
	}
	forward := model.Forward{Kind: kind, Bind: net.JoinHostPort(bindHost, bindPort)}
	if auto {
		forward.Bind, forward.ListenPort = "", "auto"
	}
	if kind != model.ForwardDynamic {
		forward.Target = net.JoinHostPort(targetHost, targetPort)
	}
	warning := ""
	if bindHost != "localhost" && !net.ParseIP(bindHost).IsLoopback() {
		warning = fmt.Sprintf("-%s listens on non-loopback address %s; other machines may reach this forward", kind, forward.Bind)
	}
	return forward, warning, nil
}

func defineValidPort(value string) bool {
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return false
	}
	n, err := strconv.Atoi(value)
	return err == nil && n >= 1 && n <= 65535
}

func defineColonParts(value string) ([]string, error) {
	var parts []string
	var b strings.Builder
	bracket := false
	for _, r := range value {
		switch r {
		case '[':
			if bracket || b.Len() != 0 {
				return nil, fmt.Errorf("invalid IPv6 brackets")
			}
			bracket = true
		case ']':
			if !bracket {
				return nil, fmt.Errorf("invalid IPv6 brackets")
			}
			bracket = false
		case ':':
			if !bracket {
				parts = append(parts, b.String())
				b.Reset()
				continue
			}
		}
		if r != '[' && r != ']' {
			b.WriteRune(r)
		}
	}
	if bracket {
		return nil, fmt.Errorf("unclosed IPv6 bracket")
	}
	return append(parts, b.String()), nil
}
