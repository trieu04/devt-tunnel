package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/api"
)

func (a *app) resolveTunnel(ctx context.Context, ref string) (api.TunnelView, error) {
	var tunnels []api.TunnelView
	if err := a.call(ctx, "tunnel.list", api.Empty{}, &tunnels); err != nil {
		return api.TunnelView{}, err
	}
	return matchTunnel(tunnels, ref)
}

func matchTunnel(tunnels []api.TunnelView, ref string) (api.TunnelView, error) {
	for _, tunnel := range tunnels {
		if tunnel.Tunnel.ID == ref {
			return tunnel, nil
		}
	}
	var names, prefixes []api.TunnelView
	for _, tunnel := range tunnels {
		if tunnel.Tunnel.Name == ref {
			names = append(names, tunnel)
		}
		if ref != "" && strings.HasPrefix(tunnel.Tunnel.ID, ref) {
			prefixes = append(prefixes, tunnel)
		}
	}
	matches := names
	if len(matches) == 0 {
		matches = prefixes
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return api.TunnelView{}, usageError("unknown tunnel %q", ref)
	}
	ids := make([]string, len(matches))
	for i, match := range matches {
		ids[i] = match.Tunnel.ID
	}
	sort.Strings(ids)
	return api.TunnelView{}, usageError("ambiguous tunnel %q; matches: %s", ref, strings.Join(ids, ", "))
}
