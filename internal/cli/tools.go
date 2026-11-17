package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/platform/downloads"
)

func init() { register(toolsCommand) }

func toolsCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "tools", Short: "List tunnel binaries and their versions",
		Long:    "Show ssh, ngrok and cloudflared paths, versions, discovery sources and errors using the daemon's environment. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version.",
		Example: "  devt-tunnel tools\n  devt-tunnel tools --json\n  devt-tunnel tools set ssh /usr/bin/ssh\n  devt-tunnel tools download cloudflared", Args: cobra.NoArgs,
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
	candidates := &cobra.Command{
		Use: "candidates <tool>", Short: "List every installed copy of a tool",
		Long:    "List each ssh, ngrok or cloudflared executable the daemon can find on its search path or in devt-tunnel's own tools directory, with version, origin, whether Auto would pick it and whether it is the selected override. Use tools set to select one.",
		Example: "  devt-tunnel tools candidates cloudflared\n  devt-tunnel tools candidates ngrok --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "ssh" && args[0] != "ngrok" && args[0] != "cloudflared" {
				return usageError("tool must be ssh, ngrok or cloudflared")
			}
			list := []api.ToolCandidate{}
			if err := a.call(cmd.Context(), "tool.candidates", api.ToolParams{Tool: args[0]}, &list); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"candidates": list})
			}
			rows := make([][]string, 0, len(list))
			for _, c := range list {
				version := c.Version
				if c.Error != "" {
					version = "error: " + c.Error
				}
				rows = append(rows, []string{c.Path, version, c.Origin, yesNo(c.Auto), yesNo(c.Selected)})
			}
			return a.table([]string{"PATH", "VERSION", "ORIGIN", "AUTO", "SELECTED"}, rows)
		},
	}
	cmd.AddCommand(candidates)
	var noUse bool
	download := &cobra.Command{
		Use: "download <tool>", Short: "Download a pinned cloudflared release",
		Long: "Download the cloudflared release pinned in this devt-tunnel build into devt-tunnel's own tools directory, check its SHA-256 and size against the values compiled into devt-tunnel, and select it as the tool override unless --no-use is given. " +
			"The daemon only fetches URLs compiled into it and never downloads on its own. A progress line is shown on stderr when it is a terminal; Ctrl-C cancels and removes the partial file. " +
			"Exits with not_found when this platform has no pinned release, checksum_mismatch when the file does not match, and network when the transfer fails.",
		Example: "  devt-tunnel tools download cloudflared\n  devt-tunnel tools download cloudflared --no-use --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "ssh" && args[0] != "ngrok" && args[0] != "cloudflared" {
				return usageError("tool must be ssh, ngrok or cloudflared")
			}
			return a.toolsDownload(cmd.Context(), args[0], !noUse)
		},
	}
	download.Flags().BoolVar(&noUse, "no-use", false, "Keep the current tool selection")
	cmd.AddCommand(download)
	remove := &cobra.Command{
		Use: "remove <tool> <version>", Short: "Delete a downloaded tool version",
		Long:    "Delete one version that devt-tunnel downloaded into its tools directory. Refused with conflict while a running tunnel uses it. If that version is the selected override, the override is cleared first so tunnels return to automatic discovery. Nothing outside the tools directory is ever deleted.",
		Example: "  devt-tunnel tools remove cloudflared 2026.11.0\n  devt-tunnel tools remove cloudflared 2026.11.0 --json", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "ssh" && args[0] != "ngrok" && args[0] != "cloudflared" {
				return usageError("tool must be ssh, ngrok or cloudflared")
			}
			if !downloads.ValidVersion(args[1]) {
				return usageError("version must be a release number such as 2026.11.0")
			}
			tools := []api.ToolInfo{}
			if err := a.call(cmd.Context(), "tool.removeDownloaded", api.ToolVersionParams{Tool: args[0], Version: args[1]}, &tools); err != nil {
				return err
			}
			return a.toolsWrite(tools)
		},
	}
	cmd.AddCommand(remove)
	return cmd
}

// toolsDownload waits for the daemon's download without the request timeout;
// Ctrl-C asks the daemon to cancel it.
func (a *app) toolsDownload(ctx context.Context, tool string, use bool) error {
	c, err := a.client(ctx)
	if err != nil {
		return err
	}
	progress := !a.json && !a.quiet && IsTerminal(a.io.Err)
	shown := false
	if progress {
		var mu sync.Mutex
		c.Events(func(event api.Event) {
			if event.Type != api.EventToolDownload || event.Download == nil || event.Download.Tool != tool {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if line := downloadLine(*event.Download); line != "" {
				fmt.Fprint(a.io.Err, "\r\x1b[K"+line)
				shown = true
			}
		})
		defer c.Events(nil)
		if err := a.call(ctx, api.EventsSubscribe, api.Empty{}, nil); err != nil {
			return err
		}
	}
	tools := []api.ToolInfo{}
	err = c.Call(ctx, "tool.download", api.ToolDownloadParams{Tool: tool, Use: use}, &tools)
	if progress {
		c.Events(nil)
		if shown {
			fmt.Fprint(a.io.Err, "\r\x1b[K")
		}
	}
	if ctx.Err() != nil {
		cancelCtx, cancel := context.WithTimeout(context.Background(), a.timeout)
		defer cancel()
		_ = c.Call(cancelCtx, "tool.downloadCancel", api.ToolParams{Tool: tool}, nil)
		return api.NewError(api.Stopped, tool+" download was cancelled")
	}
	if err != nil {
		return err
	}
	return a.toolsWrite(tools)
}

func downloadLine(p api.ToolDownloadProgress) string {
	mb := func(n int64) string { return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) }
	switch p.State {
	case api.DownloadDownloading:
		return fmt.Sprintf("Downloading %s %s  %s / %s MB", p.Tool, p.Version, mb(p.Received), mb(p.Total))
	case api.DownloadVerifying:
		return fmt.Sprintf("Verifying %s %s checksum…", p.Tool, p.Version)
	}
	return ""
}

func (a *app) toolsWrite(tools []api.ToolInfo) error {
	if a.json {
		return a.writeJSON(map[string]any{"tools": tools})
	}
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		rows = append(rows, []string{tool.Name, tool.Path, tool.Version, tool.Origin, tool.Source, tool.Error})
	}
	return a.table([]string{"NAME", "PATH", "VERSION", "ORIGIN", "SOURCE", "ERROR"}, rows)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
