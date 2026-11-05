package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAgentDefaults(t *testing.T) {
	want := Agent{Enabled: true, PortRange: "20000-20999", DefaultTTL: "30m", MaxTTL: "8h", AllowTunnels: []string{}, AllowSSHHosts: []string{}, Public: "ask"}
	for _, input := range []string{"", "shortcut = 'Ctrl+K'\n", "[agent]\n", "[agent]\npublic = 'ask'\n"} {
		t.Run(input, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "settings.toml")
			if err := os.WriteFile(p, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Load(p)
			if err != nil || s.Agent == nil || !reflect.DeepEqual(s.AgentPolicy(), want) {
				t.Fatalf("agent=%+v err=%v", s.Agent, err)
			}
		})
	}
	missing, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil || !reflect.DeepEqual(missing.AgentPolicy(), want) || !reflect.DeepEqual((Settings{}).AgentPolicy(), want) {
		t.Fatalf("missing agent=%+v err=%v", missing.Agent, err)
	}
}

func TestAgentRoundTrip(t *testing.T) {
	for _, agent := range []Agent{
		DefaultAgent(),
		{Enabled: false, PortRange: "31000-31009", DefaultTTL: "5m", MaxTTL: "2h", AllowTunnels: []string{"dev-*", "prod-db"}, AllowSSHHosts: []string{"staging", "bastion"}, Public: "deny"},
	} {
		t.Run(agent.Public, func(t *testing.T) {
			updates := false
			want := Settings{Tools: map[string]string{"ssh": "/tools/ssh"}, Shortcut: "Ctrl+K", Shell: "bash", CheckUpdates: &updates, Agent: &agent}
			p := filepath.Join(t.TempDir(), "settings.toml")
			if err := Save(p, want); err != nil {
				t.Fatal(err)
			}
			got, err := Load(p)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%+v agent=%+v err=%v; want=%+v", got, got.Agent, err, want)
			}
			text, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"[agent]", "enabled =", "port_range =", "default_ttl =", "max_ttl =", "allow_tunnels =", "allow_ssh_hosts =", "public ="} {
				if !strings.Contains(string(text), key) {
					t.Errorf("missing %q in %s", key, text)
				}
			}
		})
	}
}

func TestAgentPartialAndDisabled(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("[agent]\nenabled = false\nallow_tunnels = ['dev-*']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultAgent()
	want.Enabled, want.AllowTunnels = false, []string{"dev-*"}
	if !reflect.DeepEqual(s.AgentPolicy(), want) {
		t.Fatalf("got=%+v want=%+v", s.AgentPolicy(), want)
	}
	if err := Save(p, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Agent.Enabled {
		t.Fatalf("disabled flag lost: %+v, %v", got.Agent, err)
	}
}

func TestSaveNilAgentUsesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.toml")
	original := Settings{Shortcut: "Ctrl+K"}
	if err := Save(p, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil || !reflect.DeepEqual(loaded.AgentPolicy(), DefaultAgent()) || original.Agent != nil {
		t.Fatalf("got=%+v original=%+v err=%v", loaded.Agent, original, err)
	}
}

func TestAgentInvalidSettings(t *testing.T) {
	cases := []struct{ input, field string }{
		{"port_range = '0-20'", "port_range"},
		{"port_range = '20999-20000'", "port_range"},
		{"port_range = '20000-65536'", "port_range"},
		{"port_range = '+1-2'", "port_range"},
		{"port_range = '20000'", "port_range"},
		{"default_ttl = 'forever'", "default_ttl"},
		{"default_ttl = '0s'", "default_ttl"},
		{"default_ttl = '-1m'", "default_ttl"},
		{"max_ttl = 'no'", "max_ttl"},
		{"max_ttl = '10m'", "max_ttl"},
		{"public = 'allow'", "public"},
		{"public = 'ASK'", "public"},
		{"allow_tunnels = ['[']", "allow_tunnels"},
		{"allow_tunnels = ['']", "allow_tunnels"},
		{"allow_ssh_hosts = ['-oProxyCommand=x']", "allow_ssh_hosts"},
		{"allow_ssh_hosts = ['two words']", "allow_ssh_hosts"},
		{"publik = 'deny'", "unknown key"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "settings.toml")
			if err := os.WriteFile(p, []byte("[agent]\n"+tc.input+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("err=%v; want %s", err, tc.field)
			}
		})
	}
}

func TestInvalidAgentSaveLeavesFileUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(p, []byte("shortcut = 'old'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := DefaultAgent()
	a.Public = "allow"
	if err := Save(p, Settings{Agent: &a}); err == nil {
		t.Fatal("invalid public policy was saved")
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "shortcut = 'old'\n" {
		t.Fatalf("file changed: %s %v", data, err)
	}
}

func TestAgentPorts(t *testing.T) {
	for _, input := range []string{"1-1", "20000-20999", "65535-65535"} {
		a := DefaultAgent()
		a.PortRange = input
		first, last, err := a.Ports()
		if err != nil || first < 1 || last < first || last > 65535 {
			t.Fatalf("Ports(%q)=%d,%d,%v", input, first, last, err)
		}
	}
}
