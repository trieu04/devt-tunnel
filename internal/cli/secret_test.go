package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestSecretRoundTripRealDaemon(t *testing.T) {
	store := &secret.Memory{}
	cfg := model.Config{Tunnels: []model.Tunnel{{ID: "web", Name: "Web service", Type: model.TypeNgrok, Ngrok: &model.Ngrok{Proto: "http", Addr: "3000", Secret: "ngrok-work"}}}}
	organizeDaemon(t, &cfg, store)
	const value = "SENTINEL-NEVER-PRINT-938492"
	out := organizeSuccess(t, value+"\n", "secret", "set", "ngrok-work", "--kind", "ngrok", "--stdin", "--no-input")
	if strings.Contains(out, value) {
		t.Fatalf("set leaked value: %s", out)
	}
	if got, err := store.Get("ngrok-work"); err != nil || got != value {
		t.Fatalf("stored=%q err=%v", got, err)
	}
	out = organizeSuccess(t, "", "secret", "ls")
	var list struct {
		Secrets []struct {
			Name, Kind, State, LastUsed string
			UsedBy                      []string
		}
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Secrets) != 1 || list.Secrets[0].Name != "ngrok-work" || list.Secrets[0].Kind != "ngrok" || list.Secrets[0].State != "present" || strings.Join(list.Secrets[0].UsedBy, ",") != "Web service" {
		t.Fatalf("ls=%s err=%v", out, err)
	}
	if strings.Contains(out, value) || strings.Contains(out, `"value"`) {
		t.Fatalf("ls leaked value: %s", out)
	}
	organizeSuccess(t, "", "secret", "rename", "ngrok-work", "ngrok-office")
	out = organizeSuccess(t, "", "secret", "ls")
	if !strings.Contains(out, "ngrok-office") || strings.Contains(out, "ngrok-work") || strings.Contains(out, value) {
		t.Fatalf("rename: %s", out)
	}
	code, out, diagnostics := organizeInvoke("", "secret", "rm", "ngrok-office")
	if code != 4 || out != "" || !strings.Contains(diagnostics, `"code":"needs_user"`) {
		t.Fatalf("unconfirmed rm: code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
	organizeSuccess(t, "", "secret", "rm", "ngrok-office", "--yes")
	if _, err := store.Get("ngrok-office"); err != secret.ErrNotFound {
		t.Fatalf("secret not deleted: %v", err)
	}
	out = organizeSuccess(t, "", "secret", "ls")
	if !strings.Contains(out, `"state":"missing"`) || !strings.Contains(out, "Web service") || strings.Contains(out, value) {
		t.Fatalf("deleted secret reference: %s", out)
	}
}

func TestSecretStdinPreservesValueExceptOneNewline(t *testing.T) {
	store := &secret.Memory{}
	organizeDaemon(t, &model.Config{}, store)
	for _, tc := range []struct{ input, want string }{
		{"  spaces  \n", "  spaces  "}, {"line one\nline two\n\n", "line one\nline two\n"},
		{"windows\r\n", "windows"}, {"no-newline", "no-newline"}, {"\n\n", "\n"},
	} {
		organizeSuccess(t, tc.input, "secret", "set", "test", "--kind", "env", "--stdin")
		if got, err := store.Get("test"); err != nil || got != tc.want {
			t.Fatalf("input=%q got=%q want=%q err=%v", tc.input, got, tc.want, err)
		}
	}
}

func TestSecretNeedsInputAndUsage(t *testing.T) {
	organizeDaemon(t, &model.Config{}, nil)
	for _, tc := range []struct {
		input string
		args  []string
		code  int
		kind  string
	}{
		{"not-consumed", []string{"set", "token", "--kind", "env"}, 4, "needs_user"},
		{"not-consumed", []string{"set", "token", "--kind", "env", "--no-input"}, 4, "needs_user"},
		{"", []string{"set", "token", "--kind", "env", "--stdin"}, 2, "usage"},
		{"\n", []string{"set", "token", "--kind", "env", "--stdin"}, 2, "usage"},
		{"\r\n", []string{"set", "token", "--kind", "env", "--stdin"}, 2, "usage"},
		{"x", []string{"set", "token", "--stdin"}, 2, "usage"},
		{"x", []string{"set", "token", "--kind", "bad", "--stdin"}, 2, "usage"},
		{"x", []string{"set", "bad name", "--kind", "env", "--stdin"}, 2, "usage"},
		{strings.Repeat("x", 2561), []string{"set", "token", "--kind", "env", "--stdin"}, 2, "usage"},
		{"", []string{"set", "token", "positional-value", "--kind", "env"}, 2, "usage"},
		{"", []string{"set", "token", "--kind", "env", "--value", "secret-value"}, 2, "usage"},
		{"", []string{"rename", "token", "token"}, 2, "usage"},
		{"", []string{"rm", "token", "--no-input"}, 4, "needs_user"},
		{"", []string{"reveal", "token"}, 2, "usage"},
	} {
		code, out, diagnostics := organizeInvoke(tc.input, append([]string{"secret"}, tc.args...)...)
		if code != tc.code || out != "" || !strings.Contains(diagnostics, `"code":"`+tc.kind+`"`) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", tc.args, code, out, diagnostics)
		}
	}
}

func TestSecretFlagsNeverAcceptCredentialValues(t *testing.T) {
	root := newRoot(newApp(IO{}, Options{}))
	secretCmd, _, err := root.Find([]string{"secret"})
	if err != nil {
		t.Fatal(err)
	}
	// Keep this explicit: any new free-form flag under secret requires a security
	// decision here. --kind is the only string flag, and its enum is checked above.
	allowed := map[string]string{
		"kind": "string", "stdin": "bool", "yes": "bool", "help": "bool",
		"json": "bool", "quiet": "bool", "no-color": "bool", "timeout": "duration",
		"no-daemon-start": "bool", "no-input": "bool",
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		check := func(flag *pflag.Flag) {
			want, ok := allowed[flag.Name]
			if !ok || flag.Value.Type() != want {
				t.Errorf("%s: flag --%s (%s) could carry a credential value", cmd.CommandPath(), flag.Name, flag.Value.Type())
			}
		}
		cmd.LocalFlags().VisitAll(check)
		cmd.InheritedFlags().VisitAll(check)
		cmd.PersistentFlags().VisitAll(check)
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(secretCmd)
}

type secretUnavailableStore struct{}

func (secretUnavailableStore) Get(string) (string, error) { return "", secret.ErrUnavailable }
func (secretUnavailableStore) Set(string, string) error   { return secret.ErrUnavailable }
func (secretUnavailableStore) Delete(string) error        { return secret.ErrUnavailable }

func TestSecretUnavailableKeychainRealDaemon(t *testing.T) {
	organizeDaemon(t, &model.Config{}, secretUnavailableStore{})
	for _, args := range [][]string{
		{"secret", "set", "token", "--kind", "env", "--stdin"},
		{"secret", "rename", "from", "to"}, {"secret", "rm", "token", "--yes"}, {"secret", "ls"},
	} {
		code, _, diagnostics := organizeInvoke("hidden-value", args...)
		if code != 3 || !strings.Contains(diagnostics, `"code":"keyring_unavailable"`) || strings.Contains(diagnostics, "hidden-value") {
			t.Errorf("%v: code=%d stderr=%s", args, code, diagnostics)
		}
	}
}
