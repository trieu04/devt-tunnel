package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/daemon"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() { register(daemonCommand) }

func daemonCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "daemon",
		Short:   "Control the per-user daemon",
		Long:    "Run or manage the per-user tunnel daemon. Stopping or restarting it stops all tunnels.",
		Example: "  dtt daemon status\n  dtt daemon restart --timeout 20s",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	idle := daemon.DefaultIdleExit
	run := &cobra.Command{
		Use:     "run",
		Short:   "Run the daemon in the foreground",
		Long:    "Run the daemon in the foreground for service units and debugging. It exits after five idle minutes by default; --idle-exit 0 disables this.",
		Example: "  dtt daemon run\n  dtt daemon run --idle-exit 0",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if idle < 0 {
				return usageError("invalid --idle-exit: must not be negative")
			}
			opts := daemon.Options{Version: a.options.Version, IdleExit: idle}
			if a.options.SecretStore != nil {
				opts.Engine = engine.Options{Secrets: a.options.SecretStore()}
			}
			if err := daemon.Run(cmd.Context(), opts); err != nil {
				return &daemonError{err}
			}
			return nil
		},
	}
	run.Flags().DurationVar(&idle, "idle-exit", idle, "Exit after idle duration; 0 disables idle exit")
	cmd.AddCommand(run)
	for _, name := range []string{"status", "stop", "restart"} {
		short, long := "Show daemon status", "Show the daemon version, API version, PID, connections and tunnel states without starting it."
		if name == "stop" {
			short, long = "Stop the daemon and all tunnels", "Stop the daemon and all tunnels, including their child processes. This replaces manually killing the daemon PID."
		}
		if name == "restart" {
			short, long = "Restart the daemon, stopping all tunnels", "Stop the daemon and all tunnels, then start the current daemon version. Also starts a daemon if none is running."
		}
		cmd.AddCommand(&cobra.Command{
			Use:     name,
			Short:   short,
			Long:    long,
			Example: "  dtt daemon " + name + "\n  dtt daemon " + name + " --json --timeout 20s",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return a.manageDaemon(cmd.Context(), name)
			},
		})
	}
	return cmd
}

func (a *app) manageDaemon(ctx context.Context, command string) error {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	socket, err := platform.ControlSocket()
	if err != nil {
		return &daemonError{err}
	}
	c, err := client.DialManagement(ctx, socket, a.info())
	if err != nil && command != "restart" {
		return &daemonError{err}
	}
	if c != nil {
		defer c.Close()
		if command == "status" {
			var status api.DaemonStatus
			if err = c.Call(ctx, api.DaemonStatusMethod, api.Empty{}, &status); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{
					"daemonVersion": status.DaemonVersion, "pid": status.PID,
					"daemonApiVersion": status.APIVersion, "active": status.Active,
					"connections": status.Connections, "tunnels": status.Tunnels,
				})
			}
			fmt.Fprintf(a.io.Out, "daemon %s (API %d), pid %d, active=%t, clients=%d\n", status.DaemonVersion, status.APIVersion, status.PID, status.Active, status.Connections)
			for _, tunnel := range status.Tunnels {
				fmt.Fprintf(a.io.Out, "%s\t%s\tpid=%d\t%s\n", tunnel.ID, tunnel.State, tunnel.PID, tunnel.Address)
			}
			return nil
		}
		if !a.json {
			a.diagnostic("Stopping daemon and all tunnels.")
		}
		if err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{ExpectedPID: c.Hello().PID}, nil); err != nil {
			return err
		}
		select {
		case <-c.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if command == "restart" {
		path, err := os.Executable()
		if err != nil {
			return &daemonError{err}
		}
		c, err = client.Ensure(ctx, client.EnsureOptions{Socket: socket, Info: a.info(), DaemonPath: path})
		if err != nil {
			return &daemonError{err}
		}
		defer c.Close()
	}
	if a.json {
		return a.writeJSON(map[string]any{"status": command + " complete"})
	}
	_, err = fmt.Fprintln(a.io.Out, "daemon", command, "complete")
	return err
}
