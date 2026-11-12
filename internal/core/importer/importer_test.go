package importer

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestParseSSHForms(t *testing.T) {
	cases := []struct {
		line  string
		check func(*testing.T, Result)
	}{
		{`ssh -N -C -T -A -q -v db`, func(t *testing.T, r Result) {
			if !reflect.DeepEqual(r.Tunnel.SSH.Flags, []string{"-N", "-C", "-T", "-A", "-q", "-v"}) {
				t.Fatalf("flags %v", r.Tunnel.SSH.Flags)
			}
		}},
		{`ssh -NTL 8080:db:5432 db`, func(t *testing.T, r Result) {
			if len(r.Tunnel.Forwards) != 1 || r.Tunnel.Forwards[0].Bind != "127.0.0.1:8080" || r.Tunnel.Forwards[0].Target != "db:5432" {
				t.Fatalf("forward %#v", r.Tunnel.Forwards)
			}
		}},
		{`ssh -L [::1]:8080:db:5432 -R 9000:svc:80 -D 1080 db`, func(t *testing.T, r Result) {
			if len(r.Tunnel.Forwards) != 3 || r.Tunnel.Forwards[0].Bind != "[::1]:8080" || r.Tunnel.Forwards[1].Kind != model.ForwardRemote || r.Tunnel.Forwards[2].Kind != model.ForwardDynamic {
				t.Fatalf("forwards %#v", r.Tunnel.Forwards)
			}
		}},
		{`ssh -o ServerAliveInterval=30 -oExitOnForwardFailure=no -o ProxyJump jump db`, func(t *testing.T, r Result) {
			if r.Tunnel.SSH.Options["ServerAliveInterval"] != "30" || r.Tunnel.SSH.Options["ExitOnForwardFailure"] != "no" || r.Tunnel.SSH.Options["ProxyJump"] != "jump" {
				t.Fatalf("options %#v", r.Tunnel.SSH.Options)
			}
		}},
		{`ssh -o ServerAliveInterval 20 -p 2222 -i /keys/mykey -l alice db`, func(t *testing.T, r Result) {
			if r.Tunnel.SSH.Hostname != "db" || r.Tunnel.SSH.User != "alice" || r.Tunnel.SSH.Port != 2222 || r.Tunnel.SSH.Identity != `/keys/mykey` || r.Tunnel.SSH.Options["ServerAliveInterval"] != "20" {
				t.Fatalf("ssh %#v", r.Tunnel.SSH)
			}
		}},
		{`ssh -f -M -S ctl -F cfg db`, func(t *testing.T, r Result) {
			if strings.Contains(r.Tunnel.SSH.ExtraArgs, "-f") || !strings.Contains(r.Tunnel.SSH.ExtraArgs, "-M") || !strings.Contains(r.Tunnel.SSH.ExtraArgs, "-F cfg") || len(r.Warnings) < 2 {
				t.Fatalf("result %+v", r)
			}
		}},
		{`ssh db echo hi`, func(t *testing.T, r Result) { _ = r }},
		{`ssh -L /tmp/sock:/var/run/db db`, func(t *testing.T, r Result) {
			if !strings.Contains(r.Tunnel.SSH.ExtraArgs, "/tmp/sock") {
				t.Fatalf("args %q", r.Tunnel.SSH.ExtraArgs)
			}
		}},
		{`ssh db`, func(t *testing.T, r Result) {
			if len(r.Warnings) != 0 {
				t.Fatalf("warnings %v", r.Warnings)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			r, err := Parse(tc.line, command.POSIX, []string{"db"})
			if strings.Contains(tc.line, "echo hi") {
				if err == nil {
					t.Fatal("expected remote command error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, r)
		})
	}
}

func TestParseSSHUnknownHostAndSSHURL(t *testing.T) {
	r, err := Parse(`ssh ssh://bob@db.example:2200`, command.POSIX, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tunnel.SSH.User != "bob" || r.Tunnel.SSH.Hostname != "db.example" || r.Tunnel.SSH.Port != 2200 {
		t.Fatalf("ssh %#v", r.Tunnel.SSH)
	}
	r, err = Parse(`ssh new-host`, command.POSIX, nil)
	if err != nil || len(r.Warnings) == 0 {
		t.Fatalf("result %+v err %v", r, err)
	}
}

func TestParseNgrokTable(t *testing.T) {
	cases := []struct {
		line, proto, addr, domain, extra string
		secret                           bool
	}{
		{`ngrok http 8080 --domain=x.example`, "http", "127.0.0.1:8080", "x.example", "", false},
		{`ngrok tcp 127.0.0.1:22 --domain x.example --authtoken=sentinel --region=us`, "tcp", "127.0.0.1:22", "x.example", "--region=us", true},
		{`ngrok tls 443 --url https://x.example`, "tls", "127.0.0.1:443", "x.example", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			r, err := Parse(tc.line, command.POSIX, nil)
			if err != nil {
				t.Fatal(err)
			}
			n := r.Tunnel.Ngrok
			if n.Proto != tc.proto || n.Addr != tc.addr || n.Domain != tc.domain || n.ExtraArgs != tc.extra || ((n.Secret != "") != tc.secret) {
				t.Fatalf("ngrok %+v", n)
			}
			if tc.secret && (r.Secrets[0].Value != "sentinel" || strings.Contains(n.ExtraArgs, "sentinel")) {
				t.Fatalf("secret leakage %+v", r)
			}
		})
	}
}

func TestParseNgrokFallback(t *testing.T) {
	r, err := Parse(`ngrok start mytunnel`, command.POSIX, nil)
	if err != nil || r.Tunnel.Type != model.TypeCommand || len(r.Warnings) == 0 {
		t.Fatalf("result %+v err %v", r, err)
	}
}

func TestExtraArgsPreserveQuotedValues(t *testing.T) {
	r, err := Parse(`ngrok http 80 --label "team west"`, command.POSIX, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := command.Build(r.Tunnel, command.Options{Dialect: command.POSIX})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArg(spec.Args, "team west") {
		t.Fatalf("quoted value lost: %q", spec.Args)
	}
}

func TestParseCloudflaredTable(t *testing.T) {
	cases := []struct {
		line  string
		check func(*testing.T, Result)
	}{
		{`cloudflared tunnel --url http://localhost:3000`, func(t *testing.T, r Result) {
			if r.Tunnel.Cloudflared.URL != "http://localhost:3000" || r.Tunnel.Cloudflared.Name != "" {
				t.Fatalf("cf %+v", r.Tunnel.Cloudflared)
			}
		}},
		{`cloudflared tunnel run demo --hostname x.example --token tok`, func(t *testing.T, r Result) {
			c := r.Tunnel.Cloudflared
			if c.Name != "demo" || c.Hostname != "x.example" || c.Secret != "cloudflared-pasted" || r.Secrets[0].Value != "tok" {
				t.Fatalf("result %+v", r)
			}
		}},
		{`cloudflared tunnel --token tok run demo`, func(t *testing.T, r Result) {
			if r.Tunnel.Cloudflared.Name != "demo" || r.Tunnel.Cloudflared.Secret == "" {
				t.Fatalf("result %+v", r)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			r, err := Parse(tc.line, command.POSIX, nil)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, r)
		})
	}
	for _, line := range []string{`cloudflared login`, `cloudflared access tcp`, `cloudflared service install`} {
		if _, err := Parse(line, command.POSIX, nil); err == nil {
			t.Errorf("%q should reject", line)
		}
	}
}

func TestInputPreparationAndShellFallback(t *testing.T) {
	r, err := Parse("$ ssh \\\n -N db", command.POSIX, []string{"db"})
	if err != nil || r.Tunnel.Type != model.TypeSSH {
		t.Fatalf("result %+v err %v", r, err)
	}
	r, err = Parse(`ssh db | cat`, command.POSIX, nil)
	if err != nil || r.Tunnel.Type != model.TypeCommand || len(r.Warnings) == 0 {
		t.Fatalf("result %+v err %v", r, err)
	}
	r, err = Parse(`echo hi`, command.POSIX, nil)
	if err != nil || r.Tunnel.Type != model.TypeCommand || r.Tunnel.Command.Line != "echo hi" {
		t.Fatalf("result %+v err %v", r, err)
	}
}

func TestParseNormalizesSmartDashes(t *testing.T) {
	for _, tc := range []struct {
		line     string
		typeWant model.TunnelType
	}{
		{`ngrok http 8080 —domain=x.example`, model.TypeNgrok},
		{`cloudflared tunnel —url http://localhost:3000`, model.TypeCloudflared},
	} {
		r, err := Parse(tc.line, command.POSIX, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.line, err)
		}
		if r.Tunnel.Type != tc.typeWant {
			t.Fatalf("Parse(%q) type = %q, want %q", tc.line, r.Tunnel.Type, tc.typeWant)
		}
	}
}

func TestSecretsNeverEnterTunnelOrExtraArgs(t *testing.T) {
	for _, line := range []string{`ngrok http 80 --authtoken=VERYSECRET`, `TUNNEL_TOKEN=VERYSECRET cloudflared tunnel run demo`, `NGROK_AUTHTOKEN=VERYSECRET ngrok http 80`} {
		r, err := Parse(line, command.POSIX, nil)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r.Tunnel)
		if strings.Contains(string(b), "VERYSECRET") {
			t.Fatalf("secret leaked in model: %s", b)
		}
		if len(r.Secrets) == 0 {
			t.Fatalf("no candidate: %+v", r)
		}
	}
}

func TestWindowsDialectAndExecutablePath(t *testing.T) {
	r, err := Parse(`C:\tools\ssh.exe -i "C:\Program Files\key" db`, command.Windows, []string{"db"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Tunnel.Type != model.TypeSSH || r.Tunnel.SSH.Identity != `C:\Program Files\key` {
		t.Fatalf("result %+v", r)
	}
}

func TestSSHBuildRoundTripAtLeastThirtyCommands(t *testing.T) {
	known := []string{"db"}
	for i := 1; i <= 30; i++ {
		line := `ssh -N -o ExampleOption=value` + strconv.Itoa(i) + ` -L 127.0.0.1:` + strconv.Itoa(6000+i) + `:db:` + strconv.Itoa(7000+i) + ` -v db`
		r, err := Parse(line, command.POSIX, known)
		if err != nil {
			t.Fatalf("Parse(%q): %v", line, err)
		}
		spec, err := command.Build(r.Tunnel, command.Options{})
		if err != nil {
			t.Fatalf("Build(%q): %v", line, err)
		}
		for _, want := range []string{"-N", "-v", "ExampleOption=value" + strconv.Itoa(i), "127.0.0.1:" + strconv.Itoa(6000+i) + ":db:" + strconv.Itoa(7000+i), "db"} {
			if !containsArg(spec.Args, want) {
				t.Errorf("command %d loses %q in %q", i, want, spec.Args)
			}
		}
	}
}

func TestCommandImportPreviewAndTokenNeverContainsValue(t *testing.T) {
	r, err := Parse(`ngrok http 8080 --authtoken=TOPSECRET`, command.POSIX, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := command.Build(r.Tunnel, command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(spec.Preview(command.POSIX), "TOPSECRET") {
		t.Fatal("preview leaked secret")
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"ssh -N db", "ngrok http 8080", "cloudflared tunnel --url http://localhost", "echo hi", "NGROK_AUTHTOKEN=secret ngrok http 80", "ssh -L [::1]:80:db:1 db"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = Parse(s, command.POSIX, nil)
		_, _ = Parse(s, command.Windows, nil)
	})
}

func TestParseSSHClusterKeepsUnmappedFlags(t *testing.T) {
	r, err := Parse(`ssh -4gNF cfg -oStrictHostKeyChecking=no host`, command.POSIX, []string{"host"})
	if err != nil {
		t.Fatal(err)
	}
	ssh := r.Tunnel.SSH
	if !reflect.DeepEqual(ssh.Flags, []string{"-N"}) {
		t.Fatalf("flags %v", ssh.Flags)
	}
	for _, want := range []string{"-4", "-g", "-F cfg"} {
		if !strings.Contains(ssh.ExtraArgs, want) {
			t.Fatalf("extra args %q lost %s", ssh.ExtraArgs, want)
		}
	}
	if ssh.Options["StrictHostKeyChecking"] != "no" {
		t.Fatalf("options %v", ssh.Options)
	}
	warned := 0
	for _, w := range r.Warnings {
		if strings.HasPrefix(w, "-F ") {
			warned++
		}
	}
	if warned != 1 {
		t.Fatalf("want one -F warning, got %v", r.Warnings)
	}
}

func TestParseSSHRejectsMissingValue(t *testing.T) {
	for _, line := range []string{`ssh host -p`, `ssh -Np`} {
		if _, err := Parse(line, command.POSIX, nil); err == nil || !strings.Contains(err.Error(), "-p requires a value") {
			t.Fatalf("%q: err %v", line, err)
		}
	}
}
