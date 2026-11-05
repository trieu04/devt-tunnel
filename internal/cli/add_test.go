package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestParsePortMap(t *testing.T) {
	for _, tc := range []struct {
		kind                model.ForwardKind
		value, bind, target string
		warning             bool
		invalid             bool
	}{
		{model.ForwardLocal, "5433:5432", "127.0.0.1:5433", "127.0.0.1:5432", false, false},
		{model.ForwardLocal, "5433:db:5432", "127.0.0.1:5433", "db:5432", false, false},
		{model.ForwardLocal, "0.0.0.0:5433:db:5432", "0.0.0.0:5433", "db:5432", true, false},
		{model.ForwardRemote, "5433:5432", "127.0.0.1:5433", "127.0.0.1:5432", false, false},
		{model.ForwardRemote, "*:8080:localhost:80", "*:8080", "localhost:80", true, false},
		{model.ForwardLocal, "[::1]:5433:[::1]:5432", "[::1]:5433", "[::1]:5432", false, false},
		{model.ForwardLocal, "5433:[2001:db8::1]:5432", "127.0.0.1:5433", "[2001:db8::1]:5432", false, false},
		{model.ForwardDynamic, "1080", "127.0.0.1:1080", "", false, false},
		{model.ForwardDynamic, "localhost:1080", "localhost:1080", "", false, false},
		{model.ForwardDynamic, "[::]:1080", "[::]:1080", "", true, false},
		{model.ForwardLocal, "auto:5432", "", "127.0.0.1:5432", false, false},
		{model.ForwardRemote, "auto:db:5432", "", "", false, true},
		{model.ForwardDynamic, "auto", "", "", false, false},
		{model.ForwardLocal, "0.0.0.0:auto:5432", "", "", false, true},
		{model.ForwardLocal, "0:5432", "", "", false, true},
		{model.ForwardLocal, "65536:5432", "", "", false, true},
		{model.ForwardLocal, "-1:5432", "", "", false, true},
		{model.ForwardLocal, "5433:0", "", "", false, true},
		{model.ForwardLocal, "5433::5432", "", "", false, true},
		{model.ForwardLocal, "5433:db:notaport", "", "", false, true},
		{model.ForwardLocal, "5433:db:5432:extra:word", "", "", false, true},
		{model.ForwardLocal, "[::1:5433:db:5432", "", "", false, true},
		{model.ForwardLocal, "5433", "", "", false, true},
		{model.ForwardDynamic, "127.0.0.1:1080:extra", "", "", false, true},
		{model.ForwardKind("X"), "5433:5432", "", "", false, true},
	} {
		t.Run(string(tc.kind)+tc.value, func(t *testing.T) {
			forward, warning, err := parsePortMap(tc.kind, tc.value)
			if tc.invalid {
				if ExitCode(err) != 2 {
					t.Fatalf("forward=%+v err=%v", forward, err)
				}
				return
			}
			if strings.Contains(tc.value, "auto") && forward.ListenPort != "auto" {
				t.Fatalf("auto map did not request allocation: %+v", forward)
			}
			if err != nil || forward.Kind != tc.kind || forward.Bind != tc.bind || forward.Target != tc.target || (warning != "") != tc.warning {
				t.Fatalf("forward=%+v warning=%q err=%v", forward, warning, err)
			}
		})
	}
}

func TestAddDryRunExactDaemonPreview(t *testing.T) {
	e := defineDaemon(t)
	for _, tc := range []struct {
		args    []string
		preview string
	}{
		{[]string{"add", "ssh", "db", "--host", "server1", "-L", "15432:5432"}, "ssh -N -o ControlMaster=no -o ControlPath=none -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -L 127.0.0.1:15432:127.0.0.1:5432 server1"},
		{[]string{"add", "ssh", "db", "--hostname", "example.com", "--user", "dev", "--port", "2222", "--identity", "/tmp/key with spaces", "-L", "15432:db:5432", "-R", "18080:8080", "-D", "1080", "--flag", "C", "--option", "ServerAliveInterval=15"}, "ssh -N -C -o ControlMaster=no -o ControlPath=none -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -L 127.0.0.1:15432:db:5432 -R 127.0.0.1:18080:127.0.0.1:8080 -D 127.0.0.1:1080 -p 2222 -i '/tmp/key with spaces' dev@example.com"},
		{[]string{"add", "ngrok", "web", "--proto", "http", "--addr", "http://127.0.0.1:3000", "--domain", "dev.example.com", "--secret", "test-auth"}, "ngrok http http://127.0.0.1:3000 --log=stdout --log-format=logfmt --domain=dev.example.com"},
		{[]string{"add", "cloudflared", "web", "--url", "http://127.0.0.1:3000"}, "cloudflared tunnel --url http://127.0.0.1:3000"},
		{[]string{"add", "cloudflared", "web", "--tunnel-name", "work", "--hostname", "dev.example.com", "--secret", "test-auth"}, "cloudflared tunnel run work"},
		{[]string{"add", "command", "proxy", "--line", "printf 'ready now'", "--dir", "/tmp", "--ready-log", "ready", "--secret-env", "AUTH=test-auth"}, `/bin/sh -c 'printf '\''ready now'\'''`},
	} {
		t.Run(strings.Join(tc.args[:3], " ")+tc.preview, func(t *testing.T) {
			args := append(append([]string{}, tc.args...), "--dry-run")
			result := defineReadResult(t, args...)
			if result.Preview != tc.preview || result.Saved || result.Started || !result.DryRun || result.ID != "" || result.BuildError != "" {
				t.Fatalf("result=%+v\nwant preview=%s", result, tc.preview)
			}
			code, out, diagnostics := defineRun(args...)
			if code != 0 || diagnostics != "" || !strings.HasPrefix(out, tc.preview+"\n") || strings.Contains(out, "never-print-this-value") {
				t.Fatalf("human dry run code=%d stdout=%s stderr=%s", code, out, diagnostics)
			}
			if len(e.Overview().Tunnels) != 0 {
				t.Fatal("dry run persisted a tunnel")
			}
		})
	}
}

func TestAddSavedBackendsRoundTrip(t *testing.T) {
	defineDaemon(t)
	for _, tc := range []struct {
		args []string
		kind model.TunnelType
	}{
		{[]string{"add", "ssh", "db", "--host", "server1", "-L", "15432:5432", "-L", "15433:db:5432"}, model.TypeSSH},
		{[]string{"add", "ngrok", "ng", "--addr", "3000", "--secret", "test-auth"}, model.TypeNgrok},
		{[]string{"add", "cloudflared", "cf", "--tunnel-name", "work", "--hostname", "dev.example.com", "--secret", "test-auth"}, model.TypeCloudflared},
		{[]string{"add", "command", "cmd", "--line", "printf ready", "--secret-env", "AUTH=test-auth", "--secret-env", "TOKEN=test-auth"}, model.TypeCommand},
	} {
		result := defineReadResult(t, tc.args...)
		saved := defineSavedTunnel(t, result.ID)
		if saved.Type != tc.kind || !saved.Reconnect || saved.ID != tc.args[2] {
			t.Fatalf("saved=%+v", saved)
		}
		switch tc.kind {
		case model.TypeSSH:
			if saved.SSH.Flags[0] != "-N" || saved.SSH.Options["ServerAliveInterval"] != "30" || len(saved.Forwards) != 2 {
				t.Fatalf("SSH defaults: %+v", saved)
			}
		case model.TypeNgrok:
			if saved.Ngrok.Secret != "test-auth" || saved.Ngrok.Proto != "http" {
				t.Fatalf("ngrok: %+v", saved.Ngrok)
			}
		case model.TypeCloudflared:
			if saved.Cloudflared.URL != "" || saved.Cloudflared.Hostname != "dev.example.com" {
				t.Fatalf("named cloudflared: %+v", saved.Cloudflared)
			}
		case model.TypeCommand:
			if !reflect.DeepEqual(saved.Command.SecretEnvs, map[string]string{"AUTH": "test-auth", "TOKEN": "test-auth"}) {
				t.Fatalf("command refs: %+v", saved.Command)
			}
		}
	}
}

func TestAddUsageValidation(t *testing.T) {
	for _, args := range [][]string{
		{"add", "ssh", "db", "--host", "server1", "--hostname", "example.com"},
		{"add", "ssh", "db", "--host", "server1", "--user", "dev"},
		{"add", "ssh", "db", "--hostname", "example.com", "--port", "0"},
		{"add", "ssh", "db", "--host", "server1", "-L", "auto:5432"},
		{"add", "ssh", "db", "--host", "server1", "--option", "missing-equals"},
		{"add", "ssh", "db", "--host", "server1", "--flag", "-p"},
		{"add", "ngrok", "web"},
		{"add", "cloudflared", "web", "--url", "http://localhost:3000", "--tunnel-name", "work"},
		{"add", "cloudflared", "web", "--url", "http://localhost:3000", "--secret", "test-auth"},
		{"add", "command", "cmd"},
		{"add", "command", "cmd", "--line", "true", "--secret-env", "MISSING_NAME="},
		{"add", "command", "cmd", "--line", "true", "--secret-env", "1BAD=test-auth"},
	} {
		if code, out, diagnostics := defineRun(args...); code != 2 {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}

func TestAddValidationBlocksSavingAndWarns(t *testing.T) {
	e := defineDaemon(t)
	for _, tc := range []struct {
		args []string
		code int
		text string
	}{
		{[]string{"add", "ngrok", "web", "--addr", "3000", "--proto", "invalid"}, 2, "ngrok proto must be"},
		{[]string{"add", "command", "proxy", "--line", "true", "--ready-log", "["}, 2, "cannot build tunnel"},
		{[]string{"add", "ngrok", "web", "--addr", "3000", "--secret", "missing"}, 1, `secret \"missing\" is missing`},
		{[]string{"add", "ssh", "db", "--host", "server1", "--flag", "f"}, 1, "detaches from supervision"},
		{[]string{"add", "ssh", "db", "--host", "server1", "--flag", "NCf"}, 1, "detaches from supervision"},
	} {
		args := append(append([]string{}, tc.args...), "--json")
		code, out, diagnostics := defineRun(args...)
		if code != tc.code || !strings.Contains(out+diagnostics, tc.text) || !strings.Contains(out, `"saved":false`) || len(e.Overview().Tunnels) != 0 {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
	result := defineReadResult(t, "add", "ssh", "public", "--host", "server1", "-L", "0.0.0.0:15432:db:5432", "--dry-run")
	warned := false
	for _, finding := range result.Findings {
		warned = warned || finding.Severity == checks.Warning && strings.Contains(finding.Message, "non-loopback")
	}
	if !warned {
		t.Fatalf("no non-loopback warning: %+v", result.Findings)
	}
}

func TestAddNeverAcceptsSecretValues(t *testing.T) {
	cmd := addCommand(newApp(IO{}, Options{}))
	found := map[string]bool{}
	for _, child := range cmd.Commands() {
		child.Flags().VisitAll(func(flag *pflag.Flag) {
			name := strings.ToLower(flag.Name)
			for _, forbidden := range []string{"token", "password", "credential", "secret-value", "secret-stdin"} {
				if strings.Contains(name, forbidden) {
					t.Errorf("%s accepts a credential flag --%s", child.Name(), flag.Name)
				}
			}
			if strings.Contains(name, "secret") {
				found[child.Name()+"/"+name] = true
				usage := strings.ToLower(flag.Usage)
				if !strings.Contains(usage, "name") || !strings.Contains(usage, "keychain") || !strings.Contains(usage, "never a secret value") {
					t.Errorf("--%s does not clearly accept names only: %s", flag.Name, flag.Usage)
				}
			}
		})
	}
	for _, expected := range []string{"ngrok/secret", "cloudflared/secret", "command/secret-env"} {
		if !found[expected] {
			t.Errorf("missing name-only flag %s", expected)
		}
	}
}

func TestAddEvaluatesExactDaemonResultNotLocalReconstruction(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		switch method {
		case "tunnel.newDraft":
			return model.Tunnel{Type: model.TypeNgrok, Reconnect: true, Ngrok: &model.Ngrok{}}, nil
		case "tunnel.evaluateDraft":
			return api.DraftResult{Preview: "daemon-specific executable --exact-profile-config", Findings: []checks.Finding{}}, nil
		default:
			return nil, api.NewError(api.Internal, "dry run must not save")
		}
	})
	result := defineReadResult(t, "add", "ngrok", "web", "--addr", "3000", "--dry-run")
	if result.Preview != "daemon-specific executable --exact-profile-config" {
		t.Fatalf("preview reconstructed locally: %q", result.Preview)
	}
}
