package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestReadOutputGoldens(t *testing.T) {
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method == "secret.list" {
			return []api.SecretView{
				{Name: "ngrok-work", Kind: secret.KindNgrok, State: "present", UsedBy: []string{"Web"}, LastUsed: "2026-11-04T12:00:00Z"},
				{Name: "ssh-key", Kind: secret.KindSSHPassphrase, State: "missing", UsedBy: []string{"Production DB", "Test DB"}},
			}, nil
		}
		return readFixture(method, params)
	})
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"show-json", []string{"show", "db-prod", "--json"}},
		{"secret-ls", []string{"secret", "ls"}},
		{"secret-ls-json", []string{"secret", "ls", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			args := append([]string{"--no-daemon-start"}, tc.args...)
			if code := Run(context.Background(), args, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 || diagnostics.Len() != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostics)
			}
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("output differs from %s; run go test ./internal/cli -run TestReadOutputGoldens -update\nwant:\n%s\ngot:\n%s", path, want, &out)
			}
		})
	}
}
