package checks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func safeAgentSSH() model.Tunnel {
	return model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Flags: []string{"-N"}, Options: map[string]string{"ServerAliveInterval": "30"}}, Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:20000", Target: "127.0.0.1:5432"}}}
}

func requireAgentError(t *testing.T, tunnel model.Tunnel, field string) {
	t.Helper()
	findings := AgentSafe(tunnel)
	for _, finding := range findings {
		if finding.Severity == Error && finding.Field == field && finding.Message != "" {
			return
		}
	}
	t.Fatalf("want error on %s, got %+v", field, findings)
}

func TestAgentSafeDefaults(t *testing.T) {
	cases := []struct {
		name   string
		tunnel model.Tunnel
	}{
		{"MCP and editor defaults", safeAgentSSH()},
		{"no extensions", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Hostname: "example.com"}}},
		{"builder defaults", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Options: map[string]string{"ExitOnForwardFailure": "yes", "ControlMaster": "no", "ControlPath": "none"}}}},
		{"case insensitive fixed defaults", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Options: map[string]string{"eXiToNfOrWaRdFaIlUrE": "YES", "CONTROLMASTER": "NO", "controlpath": "NONE"}}}},
		{"keepalive integers", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Options: map[string]string{"serveraliveinterval": "1", "SERVERALIVECOUNTMAX": "3"}}}},
		{"ngrok", model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "3000", Secret: "token-name"}}},
		{"cloudflared", model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://127.0.0.1:3000"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := AgentSafe(tc.tunnel); len(findings) != 0 {
				t.Fatalf("unexpected findings: %+v", findings)
			}
		})
	}
}

func TestAgentSafeRejectsTunnelAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		tunnel      model.Tunnel
	}{
		{"command", "type", model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: "echo unsafe"}}},
		{"unknown", "type", model.Tunnel{Type: "other"}},
		{"missing SSH", "ssh", model.Tunnel{Type: model.TypeSSH}},
		{"missing ngrok", "ngrok", model.Tunnel{Type: model.TypeNgrok}},
		{"missing cloudflared", "cloudflared", model.Tunnel{Type: model.TypeCloudflared}},
		{"manual identity", "ssh.identity", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Hostname: "example.com", Identity: "~/.ssh/id_ed25519"}}},
		{"identity ignored by builder with alias", "ssh.identity", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging", Identity: "~/.ssh/id_ed25519"}}},
		{"destination option injection", "ssh.host", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "-oProxyCommand=malicious"}}},
		{"manual option injection", "ssh.hostname", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Hostname: "-F/tmp/config"}}},
		{"user option injection", "ssh.user", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{User: "-oProxyCommand=malicious", Hostname: "example.com"}}},
		{"host newline", "ssh.host", model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{Host: "staging\nunsafe"}}},
	} {
		t.Run(tc.name, func(t *testing.T) { requireAgentError(t, tc.tunnel, tc.field) })
	}
}

func TestAgentSafeRejectsFlags(t *testing.T) {
	for _, flags := range [][]string{
		{"-C"}, {"-T"}, {"-A"}, {"-v"}, {"-f"}, {"-n"}, {"-NC"}, {"-N", "-q"},
		{"-oProxyCommand=malicious"}, {"-o", "ProxyCommand=malicious"},
		{"-F/tmp/config"}, {"-F", "/tmp/config"}, {"-J", "jump"}, {"-o"}, {""},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			tunnel := safeAgentSSH()
			tunnel.SSH.Flags = flags
			requireAgentError(t, tunnel, "ssh.flags")
		})
	}
}

func TestAgentSafeRejectsOptionsCaseInsensitively(t *testing.T) {
	for _, option := range []struct{ key, value string }{
		{"ProxyCommand", "malicious"}, {"proxycommand", "malicious"}, {"pRoXyCoMmAnD", "malicious"},
		{"LocalCommand", "malicious"}, {"localcommand", "malicious"}, {"PERMITLOCALCOMMAND", "yes"},
		{"ProxyJump", "jump"}, {"proxyjump", "none"}, {"Include", "/tmp/config"},
		{"-F", "/tmp/config"}, {"-F/tmp/config", ""}, {"-oProxyCommand=malicious", ""},
		{"ProxyCommand malicious", ""}, {"ProxyCommand=malicious", ""}, {"", ""},
		{"ExitOnForwardFailure", "no"}, {"ControlMaster", "auto"}, {"ControlPath", "/tmp/control"},
		{"ControlPath", "none\nProxyCommand=malicious"}, {"ControlMaster", " no"},
		{"ServerAliveInterval", "0"}, {"ServerAliveInterval", "-1"}, {"ServerAliveInterval", "+1"},
		{"ServerAliveInterval", "30s"}, {"ServerAliveInterval", "1 2"}, {"ServerAliveInterval", "１"},
		{"ServerAliveCountMax", ""}, {"ServerAliveCountMax", "1\nProxyCommand=malicious"},
		{"ServerAliveCountMax", "18446744073709551616"},
		{"IdentityFile", "~/.ssh/key"}, {"RemoteCommand", "malicious"}, {"SessionType", "default"},
	} {
		t.Run(option.key+"="+option.value, func(t *testing.T) {
			tunnel := safeAgentSSH()
			tunnel.SSH.Options = map[string]string{option.key: option.value}
			requireAgentError(t, tunnel, "ssh.options")
		})
	}
}

func TestAgentSafeRejectsAllExtraArgs(t *testing.T) {
	// Blanket rejection must not depend on an incomplete parser or blacklist.
	for _, args := range []string{
		`-oProxyCommand=malicious`, `-oproxycommand=malicious`, `-oProxyCommand`,
		`-o ProxyCommand malicious`, `-o ProxyCommand=malicious`, `-o "ProxyCommand malicious"`,
		`-o pRoXyCoMmAnD=malicious`, `-o "ProxyCommand=malicious arg"`,
		`-oLocalCommand=malicious`, `-o localcommand malicious`, `-o PermitLocalCommand=yes`,
		`-o ProxyJump=jump`, `-J jump`, `-F`, `-F /tmp/config`, `-F/tmp/config`, `-f/tmp/config`,
		`-o`, `-N`, `-o ControlMaster=no`, `-oServerAliveInterval=30`, ` `, `"unterminated`,
		"-o\tProxyCommand=malicious", "\n-oProxyCommand=malicious", `-- -oProxyCommand=malicious`,
	} {
		t.Run(args, func(t *testing.T) {
			tunnel := safeAgentSSH()
			tunnel.SSH.ExtraArgs = args
			requireAgentError(t, tunnel, "ssh.extraArgs")
		})
	}
	for _, args := range []string{"--config /tmp/config", "--log=stdout", " "} {
		t.Run("ngrok "+args, func(t *testing.T) {
			requireAgentError(t, model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{ExtraArgs: args}}, "ngrok.extraArgs")
		})
		t.Run("cloudflared "+args, func(t *testing.T) {
			requireAgentError(t, model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{ExtraArgs: args}}, "cloudflared.extraArgs")
		})
	}
}

func TestAgentSafeDeterministicAndDoesNotMutate(t *testing.T) {
	tunnel := safeAgentSSH()
	tunnel.SSH.Options = map[string]string{"ProxyCommand": "not-logged", "LocalCommand": "not-logged", "ServerAliveInterval": "30"}
	before, err := json.Marshal(tunnel)
	if err != nil {
		t.Fatal(err)
	}
	want := AgentSafe(tunnel)
	for range 50 {
		if got := AgentSafe(tunnel); !reflect.DeepEqual(got, want) {
			t.Fatalf("nondeterministic: got %+v want %+v", got, want)
		}
	}
	after, err := json.Marshal(tunnel)
	if err != nil || string(before) != string(after) {
		t.Fatalf("mutated tunnel: before=%s after=%s err=%v", before, after, err)
	}
}

func TestAgentSafePassphraseRequiresConfigAlias(t *testing.T) {
	for _, tc := range []struct {
		host, hostname string
		safe           bool
	}{
		{"trusted", "", true}, {"", "evil.example", false}, {"trusted", "evil.example", false}, {"", "", false},
	} {
		tunnel := safeAgentSSH()
		tunnel.SSH.Host, tunnel.SSH.Hostname, tunnel.SSH.PassphraseSecret = tc.host, tc.hostname, "key"
		if tc.safe {
			if got := AgentSafe(tunnel); len(got) != 0 {
				t.Fatalf("alias rejected: %+v", got)
			}
		} else {
			requireAgentError(t, tunnel, "ssh.passphraseSecret")
		}
	}
}

func TestAgentSafeForwardAddressGrammar(t *testing.T) {
	for _, tc := range []struct {
		address string
		safe    bool
	}{
		{"24322", true}, {"127.0.0.1:24322", true}, {":24322", true}, {"localhost:24322", true}, {"*:24322", true},
		{"[::1]:24322", true}, {"[2001:db8::1]:24322", true}, {"/tmp/forward.sock", true}, {"./forward.sock", true},
		{"0.0.0.0:8081:x/y", false}, {"127.0.0.1:24322:host:5432", false}, {"[::1]:24322:x/y", false},
		{"127.0.0.1:24322/part", false}, {"relative/socket", false}, {"/tmp/socket:24322", false},
		{"[host]:24322", false}, {"[[::1]]:24322", false}, {"[::1%lo0]:24322", false}, {"::1:24322", false},
		{"localhost]:24322", false}, {"[localhost:24322", false}, {"24322\n", false}, {"host name:24322", false},
		{"host:0", false}, {"host:65536", false}, {"host:bad", false}, {"-bad:24322", false},
	} {
		for _, field := range []string{"bind", "target"} {
			t.Run(field+"/"+tc.address, func(t *testing.T) {
				tunnel := safeAgentSSH()
				if field == "bind" {
					tunnel.Forwards[0].Bind = tc.address
				} else {
					tunnel.Forwards[0].Target = tc.address
				}
				if tc.safe {
					if got := AgentSafe(tunnel); len(got) != 0 {
						t.Fatalf("valid address rejected: %+v", got)
					}
				} else {
					requireAgentError(t, tunnel, "forwards."+field)
				}
			})
		}
	}
}

func TestAgentSafeToolArgumentsAndManualHostnames(t *testing.T) {
	for _, value := range []string{"-option", "has space", "line\nbreak", "tab\targument", "nonbreaking space", "control\x01"} {
		for _, field := range []string{"ngrok.addr", "ngrok.domain", "cloudflared.name", "cloudflared.url", "cloudflared.hostname"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				tunnel := model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "3000"}, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000"}}
				switch field {
				case "ngrok.addr":
					tunnel.Ngrok.Addr = value
				case "ngrok.domain":
					tunnel.Ngrok.Domain = value
				case "cloudflared.name":
					tunnel.Cloudflared.Name = value
				case "cloudflared.url":
					tunnel.Cloudflared.URL = value
				case "cloudflared.hostname":
					tunnel.Cloudflared.Hostname = value
				}
				section := strings.Split(field, ".")[0]
				requireAgentError(t, tunnel, section)
			})
		}
	}
	for _, tc := range []struct {
		host string
		safe bool
	}{
		{"host.example", true}, {"host_name", true}, {"127.0.0.1", true}, {"::1", true}, {"[::1]", true},
		{"bad/path", false}, {"bad@host", false}, {"host;command", false}, {"host'quote", false}, {"[host]", false}, {"host name", false}, {"-option", false},
	} {
		tunnel := safeAgentSSH()
		tunnel.SSH.Host = ""
		tunnel.SSH.Hostname = tc.host
		if tc.safe {
			if got := AgentSafe(tunnel); len(got) != 0 {
				t.Fatalf("safe hostname %s rejected: %+v", tc.host, got)
			}
		} else {
			requireAgentError(t, tunnel, "ssh.hostname")
		}
	}
}
