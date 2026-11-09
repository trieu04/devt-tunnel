package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func init() { register(listCommand) }

type listFilter struct {
	running, favorites bool
	kind, profile      string
	group              string
}

func listCommand(a *app) *cobra.Command {
	var filter listFilter
	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List tunnels and their endpoints",
		Long:    "List saved and ephemeral tunnels, their states, endpoints and ownership. Replaces ps aux | grep ssh and lsof -i :PORT. JSON includes pinned, ephemeral and lease ownership fields.",
		Example: "  devt-tunnel ls\n  devt-tunnel ls --running --type ssh\n  devt-tunnel ls --profile work --favorites --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if filter.kind != "" && !slices.Contains([]string{"ssh", "ngrok", "cloudflared", "command"}, filter.kind) {
				return usageError("unknown tunnel type %q", filter.kind)
			}
			var tunnels []api.TunnelView
			if err := a.call(cmd.Context(), "tunnel.list", api.Empty{}, &tunnels); err != nil {
				return err
			}
			return a.writeTunnelList(filterTunnels(tunnels, filter))
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&filter.running, "running", false, "Only running tunnels")
	flags.StringVar(&filter.kind, "type", "", "Filter by ssh, ngrok, cloudflared or command")
	flags.StringVar(&filter.profile, "profile", "", "Filter by profile ID")
	flags.StringVar(&filter.group, "group", "", "Filter by group ID")
	flags.BoolVar(&filter.favorites, "favorites", false, "Only favorite tunnels")
	return cmd
}

func filterTunnels(tunnels []api.TunnelView, filter listFilter) []api.TunnelView {
	out := make([]api.TunnelView, 0, len(tunnels))
	for _, tunnel := range tunnels {
		t := tunnel.Tunnel
		if filter.running && tunnel.Status.State != supervisor.StateRunning || filter.favorites && !t.Favorite ||
			filter.kind != "" && string(t.Type) != filter.kind || filter.profile != "" && t.Profile != filter.profile ||
			filter.group != "" && !slices.Contains(t.Groups, filter.group) {
			continue
		}
		out = append(out, tunnel)
	}
	return out
}

func (a *app) writeTunnelList(tunnels []api.TunnelView) error {
	if a.json {
		type summary struct {
			ID        string           `json:"id"`
			Name      string           `json:"name"`
			Type      model.TunnelType `json:"type"`
			State     supervisor.State `json:"state"`
			PID       int              `json:"pid,omitempty"`
			Endpoints []api.Endpoint   `json:"endpoints"`
			Owner     api.TunnelOwner  `json:"owner"`
		}
		out := make([]summary, 0, len(tunnels))
		for _, view := range tunnels {
			endpoints := view.Endpoints
			if endpoints == nil {
				endpoints = []api.Endpoint{}
			}
			out = append(out, summary{view.Tunnel.ID, view.Tunnel.Name, view.Tunnel.Type, view.Status.State, view.Status.PID, endpoints, view.Owner})
		}
		return a.writeJSON(map[string]any{"tunnels": out})
	}
	rows := make([][]string, 0, len(tunnels))
	for _, view := range tunnels {
		var endpoints []string
		for _, endpoint := range view.Endpoints {
			if endpoint.Listen != "" {
				endpoints = append(endpoints, endpoint.Listen)
			}
		}
		address := strings.Join(endpoints, ", ")
		if address == "" {
			address = "-"
		}
		rows = append(rows, []string{view.Tunnel.ID, string(view.Tunnel.Type), string(view.Status.State), address})
	}
	return a.table([]string{"ID", "TYPE", "STATE", "ENDPOINT"}, rows)
}
