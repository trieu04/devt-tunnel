package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
)

func init() { register(toolsCommand) }

func toolsCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "tools", Short: "List tunnel binaries and their versions",
		Long:    "Show ssh, ngrok and cloudflared paths, versions, discovery sources and errors using the daemon's environment. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version.",
		Example: "  devt-tunnel tools\n  devt-tunnel tools --json\n  devt-tunnel tools set ssh /usr/bin/ssh", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tools := []api.ToolInfo{}
			if err := a.call(cmd.Context(), "tool.list", api.Empty{}, &tools); err != nil {
				return err
			}
			return a.toolsWrite(tools)
		},
	}
	var clear bool
	set := &cobra.Command{
		Use: "set <tool> <path> | set <tool> --clear", Short: "Set or clear a per-machine tool path",
		Long:    "Set an absolute executable path for ssh, ngrok or cloudflared. Pass an empty path or --clear to remove the override and restore automatic discovery. Returns the refreshed tool list, including version or discovery errors.",
		Example: "  devt-tunnel tools set ngrok /opt/homebrew/bin/ngrok\n  devt-tunnel tools set ssh --clear\n  devt-tunnel tools set ssh '' --json", Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "ssh" && args[0] != "ngrok" && args[0] != "cloudflared" {
				return usageError("tool must be ssh, ngrok or cloudflared")
			}
			if clear && len(args) != 1 || !clear && len(args) != 2 {
				return usageError("specify a tool path or --clear, but not both")
			}
			path := ""
			if !clear {
				path = args[1]
			}
			if path != "" && !filepath.IsAbs(path) {
				return usageError("tool path must be absolute")
			}
			tools := []api.ToolInfo{}
			if err := a.call(cmd.Context(), "tool.setPath", api.ToolPathParams{Tool: args[0], Path: path}, &tools); err != nil {
				return err
			}
			return a.toolsWrite(tools)
		},
	}
	set.Flags().BoolVar(&clear, "clear", false, "Reset to automatic tool discovery")
	cmd.AddCommand(set)
	return cmd
}

func (a *app) toolsWrite(tools []api.ToolInfo) error {
	if a.json {
		return a.writeJSON(map[string]any{"tools": tools})
	}
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		rows = append(rows, []string{tool.Name, tool.Path, tool.Version, tool.Source, tool.Error})
	}
	return a.table([]string{"NAME", "PATH", "VERSION", "SOURCE", "ERROR"}, rows)
}
