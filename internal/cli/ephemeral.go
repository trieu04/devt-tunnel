package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func init() {
	register(runCommand)
	register(forwardCommand)
	register(exposeCommand)
	register(saveCommand)
}

const backgroundLeaseHelp = "Background tunnels use a detached, revocable person lease, not a pin. They survive CLI exit until dtt stop or lease revocation; --ttl adds an expiry (maximum 8h). Foreground leases default to 30m and Ctrl-C closes the lease."

type ephemeralOptions struct {
	ttl, save, profile string
	foreground, dryRun bool
}

func (o *ephemeralOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.ttl, "ttl", "", "Stop after a positive duration, at most 8h (background default: no expiry; foreground: 30m)")
	cmd.Flags().BoolVar(&o.dryRun, "dry-run", false, "Print the evaluated command and findings without starting (auto ports need a concrete port for preview)")
}

func (o ephemeralOptions) validate(cmd *cobra.Command) error {
	if cmd.Flags().Changed("ttl") {
		d, err := time.ParseDuration(o.ttl)
		if err != nil || d <= 0 || d > 8*time.Hour {
			return usageError("--ttl must be a positive duration at most 8h")
		}
	}
	if cmd.Flags().Changed("save") && strings.TrimSpace(o.save) == "" {
		return usageError("--save must not be empty")
	}
	return nil
}

func runCommand(a *app) *cobra.Command {
	var o ephemeralOptions
	var detach bool
	var name string
	cmd := &cobra.Command{
		Use: "run -- <cmd> [args...]", Short: "Supervise a traditional tunnel command in the foreground",
		Long:    "Import ssh -N -L ..., ngrok http ... or cloudflared tunnel ... using the same importer as dtt import, then start an ephemeral tunnel and print its address when ready. SSH questions appear inline on a TTY. Inline credentials are rejected. --detach returns once ready. " + backgroundLeaseHelp,
		Example: "  dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1\n  dtt run --detach --ttl 15m -- ngrok http 3000\n  dtt run --dry-run -- cloudflared tunnel --url http://localhost:3000",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 0 {
				return usageError("put the traditional command after --")
			}
			if err := o.validate(cmd); err != nil {
				return err
			}
			if cmd.Flags().Changed("name") && strings.TrimSpace(name) == "" {
				return usageError("--name must not be empty")
			}
			line := args[0]
			if len(args) > 1 {
				line = defineJoinArgs(args)
			}
			var imported importer.Result
			if err := a.call(cmd.Context(), "tunnel.import", api.ImportParams{Line: line}, &imported); err != nil {
				return err
			}
			if len(imported.Secrets) > 0 {
				return usageError("import contains inline credentials; use dtt secret set and an existing secret name instead")
			}
			t := imported.Tunnel
			if t.Type != model.TypeSSH && t.Type != model.TypeNgrok && t.Type != model.TypeCloudflared {
				return usageError("run supports ssh, ngrok and cloudflared; use dtt add command for other programs and explicit readiness")
			}
			var defaults model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: t.Type}, &defaults); err != nil {
				return err
			}
			t.ID, t.Reconnect, t.Name = "", defaults.Reconnect, name
			if t.Name == "" {
				t.Name = string(t.Type) + " tunnel"
			}
			o.foreground = !detach
			return a.runEphemeral(cmd.Context(), t, o, imported.Warnings)
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "Return once ready, retaining a detached lease until --ttl, stop or revoke")
	cmd.Flags().StringVar(&name, "name", "", "Display name (default: TYPE tunnel)")
	o.flags(cmd)
	return cmd
}

func forwardCommand(a *app) *cobra.Command {
	var o ephemeralOptions
	var remote, socks []string
	cmd := &cobra.Command{
		Use: "forward <host> [map...]", Short: "Open a background ephemeral SSH forward",
		Long:    "Replace ssh -f -N -L 127.0.0.1:5433:127.0.0.1:5432 server1 without using ssh -f. Maps accept port:port, port:host:port or bind:port:host:port; auto:5432 requests a free loopback port from the daemon. Non-loopback binds warn. Repeat --remote or --socks for remote forwards and SOCKS listeners. SSH config aliases (including user@host destinations) are handled by ssh, using a profile's SSH config with --profile. --save promotes after readiness without restarting. " + backgroundLeaseHelp,
		Example: "  dtt forward server1 auto:5432 --json\n  dtt forward server1 5433:db:5432 --ttl 15m --save staging-db\n  dtt forward server1 --remote 8080:3000 --socks 1080 --foreground",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.validate(cmd); err != nil {
				return err
			}
			if strings.TrimSpace(args[0]) == "" || strings.HasPrefix(args[0], "-") {
				return usageError("host must be an SSH destination")
			}
			var forwards []model.Forward
			var warnings []string
			for _, group := range []struct {
				kind model.ForwardKind
				maps []string
			}{
				{model.ForwardLocal, args[1:]}, {model.ForwardRemote, remote}, {model.ForwardDynamic, socks},
			} {
				for _, value := range group.maps {
					f, warning, err := parsePortMap(group.kind, value)
					if err != nil {
						return err
					}
					forwards = append(forwards, f)
					if warning != "" {
						warnings = append(warnings, warning)
					}
				}
			}
			if len(forwards) == 0 {
				return usageError("provide a port map, --remote or --socks")
			}
			t, err := a.newSSHDraft(cmd.Context(), args[0], forwards)
			if err != nil {
				return err
			}
			t.SSH.Host = args[0]
			return a.runEphemeral(cmd.Context(), t, o, warnings)
		},
	}
	cmd.Flags().StringVar(&o.profile, "profile", "", "Use this profile's SSH config (ID, name or unique ID prefix)")
	cmd.Flags().StringArrayVar(&remote, "remote", nil, "Remote port:port, port:host:port or bind:port:host:port (repeatable)")
	cmd.Flags().StringArrayVar(&socks, "socks", nil, "SOCKS listen port, bind:port or auto (repeatable)")
	cmd.Flags().BoolVar(&o.foreground, "foreground", false, "Hold the CLI open; Ctrl-C closes its lease")
	cmd.Flags().StringVar(&o.save, "save", "", "Promote to a saved tunnel with this name after readiness")
	o.flags(cmd)
	return cmd
}

func exposeCommand(a *app) *cobra.Command {
	var o ephemeralOptions
	var via, domain, secret string
	cmd := &cobra.Command{
		Use: "expose <port|url>", Short: "Expose a local service publicly through ngrok or cloudflared",
		Long:    "Replace ngrok http 3000 or cloudflared tunnel --url http://localhost:3000. Always warns on stderr that the service will be public. --via is required: settings.toml currently has no default_expose setting. --secret is a keychain secret NAME, never a token. ngrok accepts --domain; cloudflared uses a quick tunnel unless --secret selects a token-based named tunnel, where --domain describes an already-configured public hostname (it does not create routing). --save promotes after readiness. " + backgroundLeaseHelp,
		Example: "  dtt expose 3000 --via cloudflared\n  dtt expose http://localhost:3000 --via ngrok --secret ngrok-work --domain demo.example.com\n  dtt expose 3000 --via ngrok --dry-run",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A safety warning, not an informational diagnostic: --quiet cannot hide it.
			if _, err := fmt.Fprintln(a.io.Err, "Warning: this service will be public."); err != nil {
				return err
			}
			if err := o.validate(cmd); err != nil {
				return err
			}
			if via != "ngrok" && via != "cloudflared" {
				return usageError("--via ngrok|cloudflared is required (default_expose is not implemented)")
			}
			if via == "cloudflared" && domain != "" && secret == "" {
				return usageError("cloudflared --domain requires --secret for a named tunnel; quick tunnels cannot select a domain")
			}
			address := args[0]
			if defineValidPort(address) {
				address = "http://127.0.0.1:" + address
			}
			u, err := url.Parse(address)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
				return usageError("target must be a port between 1 and 65535 or an http(s) URL without credentials")
			}
			var t model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: model.TunnelType(via)}, &t); err != nil {
				return err
			}
			t.Name = "Expose " + args[0]
			if via == "ngrok" {
				t.Ngrok.Proto, t.Ngrok.Addr, t.Ngrok.Domain, t.Ngrok.Secret = "http", address, domain, secret
			} else {
				t.Cloudflared.URL, t.Cloudflared.Secret, t.Cloudflared.Hostname = address, secret, domain
			}
			return a.runEphemeral(cmd.Context(), t, o, nil)
		},
	}
	cmd.Flags().StringVar(&via, "via", "", "Public service: ngrok or cloudflared (required)")
	cmd.Flags().StringVar(&domain, "domain", "", "ngrok domain, or already-routed hostname for a token-based named Cloudflare tunnel")
	cmd.Flags().StringVar(&secret, "secret", "", "Existing keychain secret NAME, never a token value")
	cmd.Flags().BoolVar(&o.foreground, "foreground", false, "Hold the CLI open; Ctrl-C closes its lease")
	cmd.Flags().StringVar(&o.save, "save", "", "Promote to a saved tunnel with this name after readiness")
	o.flags(cmd)
	return cmd
}

func (a *app) runEphemeral(ctx context.Context, t model.Tunnel, o ephemeralOptions, warnings []string) (err error) {
	// Membership makes the supervisor select that profile's SSH config.
	if o.profile != "" {
		p, err := a.resolveProfile(ctx, o.profile)
		if err != nil {
			return err
		}
		t.Profile = p.ID
	}
	if o.dryRun {
		for _, f := range t.Forwards {
			if f.ListenPort == "auto" {
				return usageError("--dry-run needs a concrete listen port: auto is allocated only when starting; use forward auto:PORT --json to inspect the allocated endpoint")
			}
		}
		return a.finishDefinition(ctx, t, defineOptions{dryRun: true}, warnings)
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintln(a.io.Err, "Warning: "+warning); err != nil {
			return err
		}
	}
	changed, cleanup, err := a.subscribePromptChanges(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	var result api.RunEphemeralResult
	if err := a.call(ctx, "tunnel.runEphemeral", api.RunEphemeralParams{Tunnel: t, TTL: o.ttl, Detached: !o.foreground}, &result); err != nil {
		return err
	}
	keep := false
	defer func() {
		// Leave unanswered SSH questions available via dtt prompts. All other
		// failures release ownership, including interruptions during readiness.
		var rpc *api.Error
		if keep || errors.As(err, &rpc) && rpc.Data.Kind == api.NeedsUser {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()
		closeErr := a.call(closeCtx, "lease.close", api.IDParams{ID: result.Lease.ID}, nil)
		// A terminal ephemeral tunnel may already have removed/revoked its lease.
		if err == nil && closeErr != nil && ctx.Err() != nil {
			err = closeErr
		}
	}()
	views := []api.TunnelView{{Tunnel: t}}
	views[0].Tunnel.ID = result.ID
	if err = a.awaitLifecycleTunnels(ctx, views, changed); err != nil {
		return err
	}
	if o.save != "" {
		var savedID string
		if err = a.call(ctx, "tunnel.promote", api.PromoteTunnelParams{ID: result.ID, Name: o.save}, &savedID); err != nil {
			return err
		}
		views[0].Tunnel.ID = savedID
		if err = a.refreshLifecycleTunnels(ctx, views); err != nil {
			return err
		}
	}
	if err = a.writeLifecycleTunnels(views); err != nil {
		return err
	}
	if !o.foreground {
		keep = true
		return nil
	}
	return a.holdEphemeral(ctx, views[0], result.Lease)
}

func (a *app) holdEphemeral(ctx context.Context, view api.TunnelView, lease api.Lease) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var leases []api.Lease
			if err := a.call(ctx, "lease.list", api.Empty{}, &leases); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			alive := false
			for _, l := range leases {
				if l.ID == lease.ID {
					if l.Revoked {
						return lifecycleError(view, api.NewError(api.LeaseRevoked, "lease was revoked or tunnel stopped"))
					}
					alive = true
				}
			}
			if !alive {
				if !time.Now().Before(lease.ExpiresAt) {
					return nil
				}
				return lifecycleError(view, api.NewError(api.Stopped, "tunnel stopped"))
			}
			var current api.TunnelView
			if err := a.call(ctx, "tunnel.get", api.IDParams{ID: view.Tunnel.ID}, &current); err != nil {
				return err
			}
			if current.Status.State != supervisor.StateRunning && current.Status.State != supervisor.StateReconnecting && current.Status.State != supervisor.StateStarting {
				return lifecycleError(current, api.NewError(api.Stopped, "tunnel stopped: "+string(current.Status.State)))
			}
		}
	}
}

func saveCommand(a *app) *cobra.Command {
	var name, profile string
	cmd := &cobra.Command{
		Use: "save <tmp-id> --name N", Short: "Promote an ephemeral tunnel to a saved definition",
		Long:    "Turn a running ephemeral tmp- tunnel into a saved tunnel, without restarting its process or losing its leases. Replaces copying a one-off ssh/ngrok/cloudflared command into configuration. Optional --profile puts it in an existing profile after promotion; saving does not pin it or remove its lease expiry.",
		Example: "  dtt save tmp-123 --name staging-db\n  dtt save tmp-123 --name staging-db --profile work --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return usageError("--name is required and must not be empty")
			}
			var profileID string
			if cmd.Flags().Changed("profile") {
				if strings.TrimSpace(profile) == "" {
					return usageError("--profile must not be empty")
				}
				p, err := a.resolveProfile(cmd.Context(), profile)
				if err != nil {
					return err
				}
				profileID = p.ID
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !view.Owner.Ephemeral {
				return usageError("%s is not an ephemeral tunnel", view.Tunnel.ID)
			}
			var id string
			if err := a.call(cmd.Context(), "tunnel.promote", api.PromoteTunnelParams{ID: view.Tunnel.ID, Name: name}, &id); err != nil {
				return err
			}
			if profileID != "" {
				var current api.TunnelView
				if err := a.call(cmd.Context(), "tunnel.get", api.IDParams{ID: id}, &current); err != nil {
					return err
				}
				current.Tunnel.Profile = profileID
				if err := a.call(cmd.Context(), "tunnel.save", api.SaveTunnelParams{Tunnel: current.Tunnel}, nil); err != nil {
					return fmt.Errorf("promoted as %s, but could not set profile: %w", id, err)
				}
			}
			if a.json {
				return a.writeJSON(map[string]any{"id": id, "saved": true})
			}
			_, err = fmt.Fprintln(a.io.Out, id)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name of the saved tunnel (required)")
	cmd.Flags().StringVar(&profile, "profile", "", "Put the promoted tunnel in an existing profile")
	return cmd
}
