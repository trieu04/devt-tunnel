package cli

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(completionCommand) }

func completionCommand(_ *app) *cobra.Command {
	return &cobra.Command{
		Use: "completion bash|zsh|fish|powershell", Short: "Generate a shell completion script",
		Long:      "Write a completion script to stdout. Source it in your shell or install it in the shell's completion directory. Tunnel and profile suggestions query only an already-running daemon, silently giving up after 250ms; completion never starts a daemon.",
		Example:   "  source <(dtt completion bash)\n  dtt completion zsh > \"${fpath[1]}/_dtt\"\n  dtt completion fish > ~/.config/fish/completions/dtt.fish\n  dtt completion powershell | Out-String | Invoke-Expression",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"}, Args: cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
		},
	}
}

// Attach after constructing the tree so command groups need no completion plumbing.
func attachCompletions(root *cobra.Command, a *app) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		switch cmd.CommandPath() {
		case "dtt show", "dtt addr", "dtt logs", "dtt check", "dtt wait", "dtt edit", "dtt dup", "dtt set":
			cmd.ValidArgsFunction = a.referenceCompletion("tunnel", false)
		case "dtt start", "dtt stop", "dtt restart", "dtt rm":
			cmd.ValidArgsFunction = a.referenceCompletion("tunnel", true)
		case "dtt profile show", "dtt profile rm":
			cmd.ValidArgsFunction = a.referenceCompletion("profile", false)
		case "dtt group show", "dtt group start", "dtt group stop", "dtt group rm":
			cmd.ValidArgsFunction = a.referenceCompletion("group", false)
		case "dtt group assign", "dtt group unassign":
			cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 0 {
					return a.completeReferences(cmd, "group", prefix)
				}
				return a.completeReferences(cmd, "tunnel", prefix)
			}
		}
		if cmd.Flags().Lookup("group") != nil {
			kind := "group"
			if cmd.CommandPath() == "dtt ls" {
				kind = "group-id" // this read flag accepts IDs only, like ls --profile
			}
			_ = cmd.RegisterFlagCompletionFunc("group", a.referenceCompletion(kind, true))
		}
		if cmd.Flags().Lookup("profile") != nil {
			kind := "profile"
			if cmd.CommandPath() == "dtt ls" || cmd.CommandPath() == "dtt hosts" {
				// These read flags accept IDs, unlike profile commands and the
				// define/lifecycle flags which also resolve names and prefixes.
				kind = "profile-id"
			}
			_ = cmd.RegisterFlagCompletionFunc("profile", a.referenceCompletion(kind, true))
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func (a *app) referenceCompletion(kind string, multiple bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if !multiple && len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return a.completeReferences(cmd, kind, prefix)
	}
}

func (a *app) completeReferences(cmd *cobra.Command, kind, prefix string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	// A separate client keeps completion read-only even without --no-daemon-start,
	// and bounds the hello handshake and list request by one shared deadline.
	completion := newApp(IO{}, a.options)
	completion.noDaemonStart = true
	completion.timeout = min(a.timeout, 250*time.Millisecond)
	defer completion.close()
	var values []string
	if kind == "tunnel" {
		var tunnels []api.TunnelView
		if err := completion.call(ctx, "tunnel.list", api.Empty{}, &tunnels); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, view := range tunnels {
			values = append(values, view.Tunnel.ID, view.Tunnel.Name)
		}
	} else if kind == "group" || kind == "group-id" {
		var groups []model.Group
		if err := completion.call(ctx, "group.list", api.Empty{}, &groups); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, group := range groups {
			values = append(values, group.ID)
			if kind != "group-id" {
				values = append(values, group.Name)
			}
		}
	} else {
		var profiles []model.Profile
		if err := completion.call(ctx, "profile.list", api.Empty{}, &profiles); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, profile := range profiles {
			values = append(values, profile.ID)
			if kind != "profile-id" {
				values = append(values, profile.Name)
			}
		}
	}
	return matchingCompletions(values, prefix), cobra.ShellCompDirectiveNoFileComp
}

func matchingCompletions(values []string, prefix string) []string {
	seen := make(map[string]bool, len(values))
	var matches []string
	for _, value := range values {
		// Newlines/tabs are completion protocol separators, not safe candidates.
		if value != "" && !strings.ContainsAny(value, "\r\n\t") && strings.HasPrefix(value, prefix) && !seen[value] {
			seen[value] = true
			matches = append(matches, value)
		}
	}
	sort.Strings(matches)
	return matches
}
