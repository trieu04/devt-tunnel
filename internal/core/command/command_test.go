package command

import (
	"reflect"
	"slices"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

var shell = Options{Shell: []string{"/bin/sh", "-c"}}

func TestSSHFromConfigAlias(t *testing.T) {
	spec, err := Build(model.Tunnel{
		Type: model.TypeSSH,
		SSH: &model.SSH{
			Host:    "server1",
			Flags:   []string{"-N", "-C"},
			Options: map[string]string{"ServerAliveInterval": "30"},
		},
		Forwards: []model.Forward{
			{Kind: model.ForwardLocal, Bind: "127.0.0.1:4053", Target: "192.168.0.24:8000"},
			{Kind: model.ForwardDynamic, Bind: "127.0.0.1:1080"},
			{Kind: model.ForwardRemote, Bind: "0.0.0.0:8080", Target: "127.0.0.1:3000"},
		},
	}, Options{SSHConfig: "/work/config"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-F", "/work/config", "-N", "-C",
		"-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=30",
		"-L", "127.0.0.1:4053:192.168.0.24:8000", "-D", "127.0.0.1:1080", "-R", "0.0.0.0:8080:127.0.0.1:3000",
		"server1",
	}
	if !reflect.DeepEqual(spec.Args, want) {
		t.Errorf("args\n got %q\nwant %q", spec.Args, want)
	}
	// -R listens remotely, so only -L and -D are checked locally.
	if !reflect.DeepEqual(spec.Ready.Ports, []string{"127.0.0.1:4053", "127.0.0.1:1080"}) {
		t.Errorf("ready ports %v", spec.Ready.Ports)
	}
	if !spec.Askpass {
		t.Error("ssh should use askpass")
	}
}

func TestSSHOptionOverridesDefault(t *testing.T) {
	spec, err := Build(model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{
		Host: "h", Options: map[string]string{"ExitOnForwardFailure": "no"},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(spec.Args, "ExitOnForwardFailure=no") || slices.Contains(spec.Args, "ExitOnForwardFailure=yes") {
		t.Errorf("args %q", spec.Args)
	}
}

func TestSSHManualKeepsExtraArgs(t *testing.T) {
	spec, err := Build(model.Tunnel{Type: model.TypeSSH, SSH: &model.SSH{
		User: "me", Hostname: "example.com", Port: 2222, Identity: "/k ey", ExtraArgs: `-J "jump host" -v`,
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tail := spec.Args[len(spec.Args)-8:]
	want := []string{"-J", "jump host", "-v", "-p", "2222", "-i", "/k ey", "me@example.com"}
	if !reflect.DeepEqual(tail, want) {
		t.Errorf("tail\n got %q\nwant %q", tail, want)
	}
	if spec.Ready.AliveFor == 0 {
		t.Error("no local port: readiness should fall back to staying alive")
	}
}

func TestCloudflaredModes(t *testing.T) {
	quick, err := Build(model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(quick.Args, []string{"tunnel", "--url", "http://localhost:3000"}) || quick.SecretEnv != nil {
		t.Errorf("quick %+v", quick)
	}
	if !quick.Ready.LogPattern.MatchString("| https://calm-river-12.trycloudflare.com |") {
		t.Error("quick readiness should match the public URL")
	}
	named, err := Build(model.Tunnel{Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{Name: "web", Secret: "cf"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(named.Args, []string{"tunnel", "run", "web"}) || named.SecretEnv["TUNNEL_TOKEN"] != "cf" {
		t.Errorf("named %+v", named)
	}
}

func TestNgrok(t *testing.T) {
	spec, err := Build(model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{
		Proto: "http", Addr: "127.0.0.1:5555", Domain: "example.com", Secret: "n",
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http", "127.0.0.1:5555", "--log=stdout", "--log-format=logfmt", "--domain=example.com"}
	if !reflect.DeepEqual(spec.Args, want) || spec.SecretEnv["NGROK_AUTHTOKEN"] != "n" {
		t.Errorf("spec %+v", spec)
	}
	if !spec.Ready.LogPattern.MatchString(`t=x lvl=info msg="started tunnel" obj=tunnels name=command_line addr=http://x url=https://example.com`) {
		t.Error("ngrok readiness should match the started line")
	}
}

func TestCommand(t *testing.T) {
	spec, err := Build(model.Tunnel{Type: model.TypeCommand, Command: &model.Command{
		Line: "server --port 1", Dir: "/w", ReadyLog: "listening", SecretEnvs: map[string]string{"TOKEN": "s"},
	}}, shell)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Tool != "/bin/sh" || !reflect.DeepEqual(spec.Args, []string{"-c", "server --port 1"}) || spec.Dir != "/w" {
		t.Errorf("spec %+v", spec)
	}
	if _, err := Build(model.Tunnel{Type: model.TypeCommand, Command: &model.Command{Line: "x", ReadyLog: "("}}, shell); err == nil {
		t.Error("invalid ready_log should fail")
	}
}

func TestPreviewNeverContainsSecretNameAsValue(t *testing.T) {
	spec, err := Build(model.Tunnel{Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "1", Secret: "my-secret"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Preview(); got != "ngrok http 1 --log=stdout --log-format=logfmt" {
		t.Errorf("preview %q", got)
	}
}

func TestPreviewQuotes(t *testing.T) {
	got := Spec{Tool: "sh", Args: []string{"-c", "echo 'hi' there"}}.Preview()
	if want := `sh -c 'echo '\''hi'\'' there'`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		``:                 nil,
		`a  b`:             {"a", "b"},
		`-o 'A=b c' "d e"`: {"-o", "A=b c", "d e"},
		`a\ b`:             {"a b"},
		`"x\"y"`:           {`x"y`},
		`''`:               {""},
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{`'open`, `"open`, `trail\`} {
		if _, err := SplitArgs(in); err == nil {
			t.Errorf("SplitArgs(%q) should fail", in)
		}
	}
}
