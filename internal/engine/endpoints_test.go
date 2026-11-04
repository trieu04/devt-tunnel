package engine

import (
	"reflect"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func TestEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tunnel  model.Tunnel
		address string
		want    []api.Endpoint
	}{
		{"ssh multiple", model.Tunnel{Type: model.TypeSSH, Forwards: []model.Forward{
			{Kind: model.ForwardLocal, Bind: "127.0.0.1:5433", Target: "127.0.0.1:5432"},
			{Kind: model.ForwardRemote, Bind: "[::]:8080", Target: "[::1]:80"},
			{Kind: model.ForwardDynamic, Bind: "[::1]:1080"},
			{Kind: model.ForwardLocal, Bind: "/tmp/test.sock", Target: "/remote/test.sock"},
		}}, "", []api.Endpoint{
			{Kind: "local", Listen: "127.0.0.1:5433", Target: "127.0.0.1:5432"},
			{Kind: "remote", Listen: "[::]:8080", Target: "[::1]:80", Public: true},
			{Kind: "socks", Listen: "[::1]:1080"},
			{Kind: "local", Listen: "/tmp/test.sock", Target: "/remote/test.sock"},
		}},
		{"ngrok", model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Addr: "3000"}}, "https://test.ngrok.app", []api.Endpoint{{Kind: "public", Listen: "https://test.ngrok.app", Target: "3000", Public: true}}},
		{"ngrok pending", model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Addr: "3000"}}, "3000", []api.Endpoint{{Kind: "public", Target: "3000", Public: true}}},
		{"cloudflared quick", model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000"}}, "https://test.trycloudflare.com", []api.Endpoint{{Kind: "public", Listen: "https://test.trycloudflare.com", Target: "http://localhost:3000", Public: true}}},
		{"cloudflared named", model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{Name: "named"}}, "https://named.example", []api.Endpoint{{Kind: "public", Listen: "https://named.example", Public: true}}},
		{"command", model.Tunnel{Type: model.TypeCommand}, "", []api.Endpoint{}},
		{"command loopback URL", model.Tunnel{Type: model.TypeCommand}, "http://localhost:3000", []api.Endpoint{{Kind: "local", Listen: "http://localhost:3000"}}},
		{"command URL", model.Tunnel{Type: model.TypeCommand}, "https://example.com", []api.Endpoint{{Kind: "public", Listen: "https://example.com", Public: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := endpoints(tc.tunnel, supervisor.Status{Address: tc.address}); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("endpoints = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestPublicBind(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:80", "[::]:80", "*:80", ":80", "192.168.1.2:80", "[2001:db8::1]:80", "host.example:80"} {
		if !publicBind(bind) {
			t.Errorf("%s should be public", bind)
		}
	}
	for _, bind := range []string{"80", "127.2.3.4:80", "localhost:80", "[::1]:80", "/tmp/socket", "[::1%lo0]:80"} {
		if publicBind(bind) {
			t.Errorf("%s should be private", bind)
		}
	}
}
