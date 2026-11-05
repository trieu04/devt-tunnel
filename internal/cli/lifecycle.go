package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func init() {
	register(startCommand)
	register(stopCommand)
	register(restartCommand)
	register(waitCommand)
}

func startCommand(a *app) *cobra.Command   { return startOrRestartCommand(a, false) }
func restartCommand(a *app) *cobra.Command { return startOrRestartCommand(a, true) }

func startOrRestartCommand(a *app, restart bool) *cobra.Command {
	verb, operation := "start", "tunnel.start"
	long := "Start saved tunnels, like ssh -N -L or ngrok http, and wait until they are ready. Starting an already active tunnel is a no-op. SSH questions are shown inline on a TTY; otherwise exit 4 and use dtt prompts. --timeout bounds the readiness wait; --no-wait returns after requesting the starts."
	if restart {
		verb, operation = "restart", "tunnel.restart"
		long = "Stop saved tunnels, then start them again and wait until ready, like kill <pid> followed by ssh -N -L or ngrok http. SSH questions are shown inline on a TTY; otherwise exit 4 and use dtt prompts. --timeout bounds the readiness wait."
	}
	var noWait bool
	cmd := &cobra.Command{
		Use: verb + " <t>...", Short: strings.ToUpper(verb[:1]) + verb[1:] + " tunnels and wait until ready",
		Long: long, Example: "  dtt " + verb + " db\n  dtt " + verb + " db web --timeout 30s\n  dtt " + verb + " db --no-wait",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, refs []string) error {
			views, err := a.lifecycleTunnels(cmd.Context(), refs)
			if err != nil {
				return err
			}
			var changed <-chan struct{}
			if !noWait {
				var cleanup func()
				changed, cleanup, err = a.subscribePromptChanges(cmd.Context())
				if err != nil {
					return err
				}
				defer cleanup()
			}
			for _, view := range views {
				if err := a.call(cmd.Context(), operation, api.IDParams{ID: view.Tunnel.ID}, nil); err != nil {
					return lifecycleError(view, err)
				}
			}
			if noWait {
				if err := a.refreshLifecycleTunnels(cmd.Context(), views); err != nil {
					return err
				}
				for _, view := range views {
					if view.Status.State == supervisor.StateKeyringUnavailable {
						return lifecycleError(view, api.NewError(api.KeyringUnavailable, "the OS keychain is unavailable"))
					}
					if len(view.MissingSecrets) != 0 && (view.Status.State == supervisor.StateStopped || view.Status.State == supervisor.StateNeedsToken) {
						return lifecycleError(view, api.NewError(api.NeedsSecret, "tunnel needs a secret"))
					}
				}
			} else if err := a.awaitLifecycleTunnels(cmd.Context(), views, changed); err != nil {
				return err
			}
			return a.writeLifecycleTunnels(views)
		},
	}
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "Return after requesting starts, without waiting for readiness")
	return cmd
}

func stopCommand(a *app) *cobra.Command {
	var all bool
	var profile string
	cmd := &cobra.Command{
		Use: "stop [t]...", Short: "Stop tunnels and their process trees",
		Long:    "Stop saved tunnels and their entire process trees, replacing kill <pid>. Select tunnel references, --all, or --profile P, but do not combine these selectors. Stopping an already stopped tunnel succeeds.",
		Example: "  dtt stop db web\n  dtt stop --all\n  dtt stop --profile work",
		RunE: func(cmd *cobra.Command, refs []string) error {
			byProfile := cmd.Flags().Changed("profile")
			if byProfile && profile == "" || all && byProfile || len(refs) != 0 && (all || byProfile) || len(refs) == 0 && !all && !byProfile {
				return usageError("choose tunnel references, --all, or --profile P")
			}
			var views []api.TunnelView
			var err error
			if all || byProfile {
				if err = a.call(cmd.Context(), "tunnel.list", api.Empty{}, &views); err != nil {
					return err
				}
			} else {
				views, err = a.lifecycleTunnels(cmd.Context(), refs)
				if err != nil {
					return err
				}
			}
			if byProfile {
				p, err := a.resolveProfile(cmd.Context(), profile)
				if err != nil {
					return err
				}
				views = filterTunnels(views, listFilter{profile: p.ID})
				if err := a.call(cmd.Context(), "profile.stop", api.IDParams{ID: p.ID}, nil); err != nil {
					return err
				}
			} else {
				for _, view := range views {
					if err := a.call(cmd.Context(), "tunnel.stop", api.IDParams{ID: view.Tunnel.ID}, nil); err != nil {
						return lifecycleError(view, err)
					}
				}
			}
			if err := a.refreshLifecycleTunnels(cmd.Context(), views); err != nil {
				return err
			}
			return a.writeLifecycleTunnels(views)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Stop all tunnels")
	cmd.Flags().StringVar(&profile, "profile", "", "Stop members of a profile (ID, name or unique ID prefix)")
	return cmd
}

func waitCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "wait <t>", Short: "Wait for a tunnel to become ready",
		Long:    "Wait for a tunnel that is starting or reconnecting to become running, instead of polling ps or a listening port with nc. Does not start a stopped tunnel. --timeout bounds the readiness wait. SSH questions use the same TTY behavior as dtt start.",
		Example: "  dtt wait db\n  dtt wait db --timeout 30s --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, refs []string) error {
			views, err := a.lifecycleTunnels(cmd.Context(), refs)
			if err != nil {
				return err
			}
			changed, cleanup, err := a.subscribePromptChanges(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()
			if err := a.awaitLifecycleTunnels(cmd.Context(), views, changed); err != nil {
				return err
			}
			return a.writeLifecycleTunnels(views)
		},
	}
}

// Resolve everything before mutating anything, and do not restart duplicate refs twice.
func (a *app) lifecycleTunnels(ctx context.Context, refs []string) ([]api.TunnelView, error) {
	views := make([]api.TunnelView, 0, len(refs))
	seen := make(map[string]bool)
	for _, ref := range refs {
		view, err := a.resolveTunnel(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !seen[view.Tunnel.ID] {
			views = append(views, view)
			seen[view.Tunnel.ID] = true
		}
	}
	return views, nil
}

func (a *app) refreshLifecycleTunnels(ctx context.Context, views []api.TunnelView) error {
	for i := range views {
		var current api.TunnelView
		if err := a.call(ctx, "tunnel.get", api.IDParams{ID: views[i].Tunnel.ID}, &current); err != nil {
			return lifecycleError(views[i], err)
		}
		views[i] = current
	}
	return nil
}

func (a *app) awaitLifecycleTunnels(ctx context.Context, views []api.TunnelView, changed <-chan struct{}) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	ids := make(map[string]bool, len(views))
	for _, view := range views {
		ids[view.Tunnel.ID] = true
	}
	// Subscribe first, then resync: an askpass question can predate this client.
	if err := a.handleTunnelPrompts(ctx, ids); err != nil {
		return err
	}
	c, err := a.client(ctx)
	if err != nil {
		return err
	}
	type result struct {
		index int
		err   error
	}
	done := make(chan result, len(views))
	// Leave room for prompt RPCs below the daemon's per-connection worker limit.
	waitSlots := make(chan struct{}, 32)
	for i, view := range views {
		go func() {
			select {
			case waitSlots <- struct{}{}:
				defer func() { <-waitSlots }()
			case <-ctx.Done():
				done <- result{i, ctx.Err()}
				return
			}
			err := c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: view.Tunnel.ID}, nil)
			done <- result{i, err}
		}()
	}
	for remaining := len(views); remaining > 0; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
			if err := a.handleTunnelPrompts(ctx, ids); err != nil {
				return err
			}
		case result := <-done:
			if result.err != nil {
				return lifecycleError(views[result.index], result.err)
			}
			remaining--
		}
	}
	return a.refreshLifecycleTunnels(ctx, views)
}

func lifecycleError(view api.TunnelView, err error) error {
	var rpc *api.Error
	var hint string
	if errors.As(err, &rpc) {
		switch rpc.Data.Kind {
		case api.NeedsSecret:
			if len(view.MissingSecrets) != 0 {
				hint = "dtt secret set " + view.MissingSecrets[0]
			} else {
				hint = "dtt show " + view.Tunnel.ID
			}
		case api.NeedsUser:
			hint = "dtt prompts"
		case api.Internal:
			// Supervisor status currently preserves only the failure message, not
			// its typed cause. Recognize the daemon's bind and tool-lookup messages.
			if strings.Contains(strings.ToLower(err.Error()), "already in use") {
				err = api.NewError(api.Conflict, err.Error())
			} else if strings.HasSuffix(err.Error(), " was not found on this machine") {
				err = api.NewError(api.NotFound, err.Error())
			}
		}
	}
	if hint != "" {
		err = fmt.Errorf("%w; run %s", err, hint)
	}
	return &tunnelError{err: err, tunnel: view.Tunnel.ID, hint: hint}
}

func (a *app) writeLifecycleTunnels(views []api.TunnelView) error {
	if a.json {
		return a.writeTunnelList(views)
	}
	if a.quiet {
		return nil
	}
	for _, view := range views {
		printed := false
		if view.Status.State == supervisor.StateRunning {
			for _, endpoint := range view.Endpoints {
				if endpoint.Listen != "" {
					if _, err := fmt.Fprintln(a.io.Out, endpoint.Listen); err != nil {
						return err
					}
					printed = true
				}
			}
		}
		if !printed {
			if _, err := fmt.Fprintf(a.io.Out, "%s %s\n", view.Tunnel.ID, view.Status.State); err != nil {
				return err
			}
		}
	}
	return nil
}
