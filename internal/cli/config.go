package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() { register(configCommand) }

func configCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "config", Short: "Locate, reload or edit tunnel configuration",
		Long:    "Work with the daemon's tunnels.toml. Reload validates disk changes before replacing the last good tunnel definitions.",
		Example: "  devt-tunnel config path\n  devt-tunnel config edit\n  devt-tunnel config reload", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use: "path", Short: "Print the tunnel configuration path",
		Long:    "Ensure tunnels.toml exists, then print its path without labels for shell substitution. Replaces finding the configuration file by hand.",
		Example: "  devt-tunnel config path\n  cat \"$(devt-tunnel config path)\"\n  devt-tunnel config path --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var path string
			if err := a.call(cmd.Context(), "config.path", api.Empty{}, &path); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"path": path})
			}
			_, err := fmt.Fprintln(a.io.Out, path)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "reload", Short: "Reload and validate tunnel configuration",
		Long:    "Reload tunnels.toml from disk and report config errors. Invalid configuration leaves the daemon's last good definitions intact and exits with code 1; concurrent config conflicts exit with code 5.",
		Example: "  devt-tunnel config reload\n  devt-tunnel config reload --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.configReload(cmd.Context(), "reloaded") },
	})
	cmd.AddCommand(&cobra.Command{
		Use: "edit", Short: "Edit tunnel configuration and reload it",
		Long:    "Open tunnels.toml in $VISUAL, falling back to $EDITOR or vi on a terminal. Editor arguments and quoted paths are supported without shell expansion. After the editor exits successfully, reload and report validation errors without replacing the last good definitions. --no-input disables editor invocation.",
		Example: "  devt-tunnel config edit\n  EDITOR='code --wait' devt-tunnel config edit", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.noInput {
				return api.NewError(api.NeedsUser, "config edit needs an editor; --no-input is enabled")
			}
			var path string
			if err := a.call(cmd.Context(), "config.path", api.Empty{}, &path); err != nil {
				return err
			}
			if err := a.configEditor(cmd.Context(), path); err != nil {
				return err
			}
			return a.configReload(cmd.Context(), "edited and reloaded")
		},
	})
	return cmd
}

func (a *app) configEditor(ctx context.Context, path string) error {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		if !a.canPrompt() {
			return api.NewError(api.NeedsUser, "config edit needs a terminal or an explicit $VISUAL/$EDITOR")
		}
		editor = "vi"
	}
	args, err := command.SplitArgs(editor, platform.Dialect())
	if err != nil || len(args) == 0 || args[0] == "" {
		return usageError("invalid $VISUAL/$EDITOR command")
	}
	process := exec.CommandContext(ctx, args[0], append(args[1:], path)...)
	process.Stdin, process.Stdout, process.Stderr = a.io.In, a.io.Out, a.io.Err
	// An editor may print messages; keep stdout reserved for the JSON result.
	if a.json {
		process.Stdout = a.io.Err
	}
	if err := process.Run(); err != nil {
		return fmt.Errorf("config editor: %w", err)
	}
	return nil
}

func (a *app) configReload(ctx context.Context, status string) error {
	reloadErr := a.call(ctx, "config.reload", api.Empty{}, nil)
	var overview api.Overview
	if err := a.call(ctx, "config.overview", api.Empty{}, &overview); err != nil {
		if reloadErr != nil {
			return reloadErr
		}
		return err
	}
	if overview.ConfigError != "" {
		if reloadErr != nil {
			return fmt.Errorf("config %s: %s: %w", overview.ConfigPath, overview.ConfigError, reloadErr)
		}
		return fmt.Errorf("config %s: %s", overview.ConfigPath, overview.ConfigError)
	}
	if reloadErr != nil {
		return reloadErr
	}
	if a.json {
		return a.writeJSON(map[string]any{"path": overview.ConfigPath, "status": status})
	}
	a.diagnostic("Configuration %s: %s.", overview.ConfigPath, status)
	return nil
}
