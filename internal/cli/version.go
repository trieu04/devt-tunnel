package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() { register(versionCommand) }

func versionCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Show CLI and reachable daemon versions",
		Long:    "Show the CLI version and API version, plus the daemon version if reachable. This command never starts a daemon.",
		Example: "  dtt version\n  dtt version --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var daemon *api.HelloResult
			ctx, cancel := context.WithTimeout(cmd.Context(), a.timeout)
			defer cancel()
			if socket, err := platform.ControlSocket(); err == nil {
				if c, err := client.DialManagement(ctx, socket, a.info()); err == nil {
					hello := c.Hello()
					daemon = &hello
					_ = c.Close()
				}
			}
			if a.json {
				fields := map[string]any{"version": a.options.Version}
				if daemon != nil {
					fields["daemon"] = daemon
				}
				return a.writeJSON(fields)
			}
			if _, err := fmt.Fprintf(a.io.Out, "dtt %s (API %d)\n", a.options.Version, api.Version); err != nil {
				return err
			}
			if daemon != nil {
				_, err := fmt.Fprintf(a.io.Out, "daemon %s (API %d), pid %d\n", daemon.DaemonVersion, daemon.APIVersion, daemon.PID)
				return err
			}
			return nil
		},
	}
}
