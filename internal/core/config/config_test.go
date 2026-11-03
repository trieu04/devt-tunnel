package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sample mirrors the example in docs/architecture.md.
const sample = `
[[profile]]
id = "work"
name = "Work"
ssh_config = "~/.ssh/config"
on_activate = "start_autostart"
stop_non_members = false
autostart = ["prod-db"]

[[profile]]
id = "home"
name = "Home"

[[tunnel]]
id = "srv1"
name = "SSH Server 1"
type = "ssh"
profiles = ["work", "home"]
favorite = true
start_on_launch = false
reconnect = true

[tunnel.ssh]
host = "server1"
flags = ["-N", "-C"]
options = { ServerAliveInterval = "30", ExitOnForwardFailure = "yes" }
extra_args = ""

[[tunnel.forward]]
kind = "L"
bind = "127.0.0.1:4053"
target = "192.168.0.24:8000"

[[tunnel]]
id = "web"
name = "Ngrok"
type = "ngrok"
profiles = ["home"]

[tunnel.ngrok]
proto = "http"
addr = "127.0.0.1:5555"
domain = "example.com"
secret = "ngrok-personal"
`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tunnels.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDocumentedExample(t *testing.T) {
	cfg, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tunnels) != 2 || len(cfg.Profiles) != 2 {
		t.Fatalf("got %d tunnels, %d profiles", len(cfg.Tunnels), len(cfg.Profiles))
	}
	srv := cfg.Tunnels[0]
	if srv.SSH.Host != "server1" || srv.SSH.Options["ServerAliveInterval"] != "30" {
		t.Errorf("ssh settings: %+v", srv.SSH)
	}
	if len(srv.Forwards) != 1 || srv.Forwards[0].Target != "192.168.0.24:8000" {
		t.Errorf("forwards: %+v", srv.Forwards)
	}
	if cfg.Tunnels[1].Ngrok.Secret != "ngrok-personal" {
		t.Errorf("ngrok: %+v", cfg.Tunnels[1].Ngrok)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	cfg, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nested", "tunnels.toml")
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, again) {
		t.Errorf("round trip changed config:\n%+v\n%+v", cfg, again)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || len(cfg.Tunnels) != 0 {
		t.Fatalf("cfg %+v err %v", cfg, err)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"unknown profile": {"[[tunnel]]\nid='a'\ntype='command'\nprofiles=['x']\n[tunnel.command]\nline='ls'\n", "unknown profile"},
		"duplicate id":    {"[[tunnel]]\nid='a'\ntype='command'\n[tunnel.command]\nline='ls'\n[[tunnel]]\nid='a'\ntype='command'\n[tunnel.command]\nline='ls'\n", "duplicate tunnel"},
		"unknown type":    {"[[tunnel]]\nid='a'\ntype='vpn'\n", "unknown type"},
		"L no target":     {"[[tunnel]]\nid='a'\ntype='ssh'\n[tunnel.ssh]\nhost='h'\n[[tunnel.forward]]\nkind='L'\nbind='1'\n", "needs a target"},
		"bad toml":        {"[[tunnel", "parse"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, c.content))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}
