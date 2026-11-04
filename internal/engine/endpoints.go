package engine

import (
	"net"
	"net/url"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func endpoints(t model.Tunnel, status supervisor.Status) []api.Endpoint {
	out := []api.Endpoint{}
	for _, f := range t.Forwards {
		kind := "local"
		switch f.Kind {
		case model.ForwardRemote:
			kind = "remote"
		case model.ForwardDynamic:
			kind = "socks"
		}
		out = append(out, api.Endpoint{Kind: kind, Listen: f.Bind, Target: f.Target, Public: publicBind(f.Bind)})
	}
	switch t.Type {
	case model.TypeNgrok:
		if t.Ngrok != nil {
			listen := status.Address
			// The supervisor uses the local target until ngrok announces its public URL.
			if listen == t.Ngrok.Addr {
				listen = ""
			}
			out = append(out, api.Endpoint{Kind: "public", Listen: listen, Target: t.Ngrok.Addr, Public: true})
		}
	case model.TypeCloudflared:
		if t.Cloudflared != nil {
			out = append(out, api.Endpoint{Kind: "public", Listen: status.Address, Target: t.Cloudflared.URL, Public: true})
		}
	case model.TypeCommand:
		if status.Address != "" && len(out) == 0 {
			public := true
			if address, err := url.Parse(status.Address); err == nil && address.Hostname() != "" {
				public = publicBind(net.JoinHostPort(address.Hostname(), address.Port()))
			}
			kind := "local"
			if public {
				kind = "public"
			}
			out = append(out, api.Endpoint{Kind: kind, Listen: status.Address, Public: public})
		}
	}
	return out
}

func publicBind(bind string) bool {
	if strings.Contains(bind, "/") {
		return false
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return false // A bare SSH port binds to loopback by default.
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if i := strings.LastIndex(host, "%"); i >= 0 {
		host = host[:i]
	}
	return !net.ParseIP(host).IsLoopback()
}
