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

func messages(f []Finding) string {
	var parts []string
	for _, x := range f {
		parts = append(parts, string(x.Severity)+": "+x.Message)
	}
	return strings.Join(parts, "\n")
}

func TestCleanSSH(t *testing.T) {
	tun := model.Tunnel{ID: "a", Type: model.TypeSSH, SSH: &model.SSH{Host: "h", Flags: []string{"-N"}}}
	if f := Run(tun, model.Config{Tunnels: []model.Tunnel{tun}}, deps()); len(f) != 0 {
		t.Errorf("findings:\n%s", messages(f))
	}
}

func TestMissingNWarns(t *testing.T) {
	tun := model.Tunnel{ID: "a", Type: model.TypeSSH, SSH: &model.SSH{Host: "h"}}
	if got := messages(Run(tun, model.Config{}, deps())); !strings.Contains(got, "warning: without -N") {
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
	if !strings.Contains(got, "127.0.0.1:1 is also used by B") {
		t.Errorf("findings:\n%s", got)
	}
	// -R listens on the remote host, so it cannot conflict locally.
	if strings.Contains(got, "used by "+remote.Name+"\n") {
		t.Errorf("remote forward flagged:\n%s", got)
	}
}
