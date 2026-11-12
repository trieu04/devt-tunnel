package checks

import (
	"errors"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func deps() Deps {
	return Deps{
		Resolve: func(tool string) (string, error) {
			if tool == "ssh" {
				return "/usr/bin/ssh", nil
			}
			return "", errors.New("missing")
		},
		Secrets: &secret.Memory{},
		Shell:   []string{"sh", "-c"},
	}
}

func TestTargetPortsNormalizesLocalServiceURLs(t *testing.T) {
	for _, tunnel := range []model.Tunnel{
		{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Addr: "http://127.0.0.1:3000"}},
		{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://127.0.0.1:3000"}},
	} {
		ports := targetPorts(tunnel)
		if len(ports) != 1 || ports[0] != "127.0.0.1:3000" {
			t.Fatalf("targetPorts(%s) = %v", tunnel.Type, ports)
		}
	}
}

func messages(f []Finding) string {
	var parts []string
	for _, x := range f {
		parts = append(parts, string(x.Severity)+": "+x.Message)
	}
	return strings.Join(parts, "\n")
}

func TestCleanSSH(t *testing.T) {
	tun := model.Tunnel{
		ID: "a", Type: model.TypeSSH,
		SSH:      &model.SSH{Host: "h", Flags: []string{"-N"}, Options: map[string]string{"ServerAliveInterval": "30"}},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:5432", Target: "localhost:5432"}},
	}
	if f := Run(tun, model.Config{Tunnels: []model.Tunnel{tun}}, deps()); len(f) != 2 || f[0].Severity != OK || f[1].Severity != OK {
		t.Errorf("findings:\n%s", messages(f))
	}
}

func TestMissingNWarns(t *testing.T) {
	tun := model.Tunnel{ID: "a", Type: model.TypeSSH, SSH: &model.SSH{Host: "h"}}
	if got := messages(Run(tun, model.Config{}, deps())); !strings.Contains(got, "warning: without -N") {
		t.Errorf("findings:\n%s", got)
	}
}

func TestSSHWithoutForwardsWarns(t *testing.T) {
	tun := model.Tunnel{ID: "a", Type: model.TypeSSH, SSH: &model.SSH{Host: "h", Flags: []string{"-N"}, Options: map[string]string{"ServerAliveInterval": "30"}}}
	if got := messages(Run(tun, model.Config{}, deps())); !strings.Contains(got, "warning: SSH tunnel has no port forwards") {
		t.Errorf("findings:\n%s", got)
	}
}

func TestMissingBinaryAndSecret(t *testing.T) {
	tun := model.Tunnel{ID: "a", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "1", Secret: "s"}}
	got := messages(Run(tun, model.Config{}, deps()))
	if !strings.Contains(got, "ngrok was not found") || !strings.Contains(got, `secret "s" is missing`) {
		t.Errorf("findings:\n%s", got)
	}
}

func TestPortConflict(t *testing.T) {
	a := model.Tunnel{ID: "a", Name: "A", Type: model.TypeSSH, SSH: &model.SSH{Host: "h", Flags: []string{"-N"}},
		Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:1", Target: "x:1"}}}
	b := a
	b.ID, b.Name = "b", "B"
	remote := a
	remote.ID, remote.Forwards = "r", []model.Forward{{Kind: model.ForwardRemote, Bind: "127.0.0.1:1", Target: "x:1"}}

	got := messages(Run(a, model.Config{Tunnels: []model.Tunnel{a, b, remote}}, deps()))
	if !strings.Contains(got, "127.0.0.1:1 conflicts with B") {
		t.Errorf("findings:\n%s", got)
	}
	// -R listens on the remote host, so it cannot conflict locally.
	if strings.Contains(got, "used by "+remote.Name+"\n") {
		t.Errorf("remote forward flagged:\n%s", got)
	}
}
