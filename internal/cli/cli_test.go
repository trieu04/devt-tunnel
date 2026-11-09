package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

var update = flag.Bool("update", false, "Update CLI golden files")

func fixtureTunnels() []api.TunnelView {
	return []api.TunnelView{
		{
			Owner:     api.TunnelOwner{Pinned: true},
			Tunnel:    model.Tunnel{ID: "db-prod", Name: "Production DB", Type: model.TypeSSH, Profile: "work", Groups: []string{"work"}, Favorite: true},
			Status:    supervisor.Status{ID: "db-prod", State: supervisor.StateRunning, PID: 4242, Owner: api.TunnelOwner{Pinned: true}},
			Endpoints: []api.Endpoint{{Kind: "local", Listen: "127.0.0.1:15432", Target: "127.0.0.1:5432"}},
			Preview:   "ssh -N -L 127.0.0.1:15432:127.0.0.1:5432 server1",
		},
		{
			Tunnel:    model.Tunnel{ID: "db-test", Name: "Test DB", Type: model.TypeSSH},
			Status:    supervisor.Status{ID: "db-test", State: supervisor.StateStopped},
			Endpoints: []api.Endpoint{{Kind: "local", Listen: "127.0.0.1:25432", Target: "127.0.0.1:5432"}},
		},
		{
			Tunnel: model.Tunnel{ID: "web", Name: "Web", Type: model.TypeCloudflared},
			Status: supervisor.Status{ID: "web", State: supervisor.StateStopped},
		},
	}
}

func TestReferenceResolution(t *testing.T) {
	tunnels := fixtureTunnels()
	// An ID wins over a name, and a name wins over a prefix.
	tunnels = append(tunnels, api.TunnelView{Tunnel: model.Tunnel{ID: "alias", Name: "db-prod"}}, api.TunnelView{Tunnel: model.Tunnel{ID: "named", Name: "db"}})
	for _, tc := range []struct {
		ref, id, message string
	}{
		{"db-prod", "db-prod", ""},
		{"Production DB", "db-prod", ""},
		{"db-p", "db-prod", ""},
		{"db", "named", ""},
		{"d", "", "db-prod, db-test"},
		{"missing", "", "unknown tunnel"},
		{"", "", "unknown tunnel"},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			view, err := matchTunnel(tunnels, tc.ref)
			if tc.message != "" {
				if ExitCode(err) != 2 || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("error = %v, code = %d", err, ExitCode(err))
				}
			} else if err != nil || view.Tunnel.ID != tc.id {
				t.Fatalf("view = %+v, error = %v", view, err)
			}
		})
	}
	tunnels[1].Tunnel.Name = "Production DB"
	if _, err := matchTunnel(tunnels, "Production DB"); ExitCode(err) != 2 || !strings.Contains(err.Error(), "db-prod, db-test") {
		t.Fatalf("duplicate names: %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	expected := map[api.ErrorKind]int{
		api.Conflict: 5, api.NeedsSecret: 3, api.KeyringUnavailable: 3,
		api.NeedsUser: 4, api.NotFound: 8, api.Timeout: 6,
		api.Incompatible: 7, api.Unknown: 2, api.Stopped: 1, api.Internal: 1, api.LeaseRevoked: 1,
		api.PolicyDenied: 1, api.AgentDisabled: 1, api.InvalidSpec: 2, api.PortInUse: 5,
	}
	for kind, want := range expected {
		if got := ExitCode(api.NewError(kind, "fixture")); got != want {
			t.Errorf("%s = %d, want %d", kind, got, want)
		}
	}
	for _, row := range ExitCodes {
		for _, kind := range row.Kinds {
			err := api.NewError(kind, "fixture")
			if got := ExitCode(err); got != row.Code {
				t.Errorf("%s = %d, want %d", kind, got, row.Code)
			}
		}
	}
	for _, tc := range []struct {
		err  error
		code int
	}{
		{nil, 0}, {errors.New("failed"), 1}, {&UsageError{Message: "bad args"}, 2},
		{context.DeadlineExceeded, 6}, {&daemonError{errors.New("cannot start")}, 7},
		{io.EOF, 7}, {net.ErrClosed, 7}, {exec.ErrNotFound, 8},
		{api.NewError("future_kind", "future"), 1},
		{&tunnelError{err: api.NewError(api.NeedsSecret, "missing"), tunnel: "db"}, 3},
		{&daemonError{context.DeadlineExceeded}, 6},
	} {
		if got := ExitCode(tc.err); got != tc.code {
			t.Errorf("%v = %d, want %d", tc.err, got, tc.code)
		}
	}
}

func TestExitCodeTableCoversAPI(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../api/protocol.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[api.ErrorKind]bool{}
	for _, row := range ExitCodes {
		for _, kind := range row.Kinds {
			covered[kind] = true
		}
	}
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			kind, ok := value.Type.(*ast.Ident)
			if !ok || kind.Name != "ErrorKind" {
				continue
			}
			literal := value.Values[0].(*ast.BasicLit)
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !covered[api.ErrorKind(text)] {
				t.Errorf("API kind %s is missing from ExitCodes", text)
			}
		}
	}
	for _, path := range []string{"../../docs/design/cli.md", "../../docs/cli/troubleshooting.md"} {
		checkDocumentedExitCodes(t, path)
	}
}

func TestCommandHelpCompleteness(t *testing.T) {
	root := newRoot(newApp(IO{}, Options{}))
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		if strings.TrimSpace(cmd.Short) == "" || strings.TrimSpace(cmd.Long) == "" || strings.TrimSpace(cmd.Example) == "" {
			t.Errorf("%s lacks Short, Long or Example", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestListGolden(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		name := "ls"
		if jsonOutput {
			name = "ls-json"
		}
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			a := newApp(IO{Out: &out}, Options{})
			a.json = jsonOutput
			if err := a.writeTunnelList(fixtureTunnels()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", name+".golden")
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
				t.Fatalf("output differs from %s; run go test ./internal/cli -update\nwant:\n%s\ngot:\n%s", path, want, &out)
			}
		})
	}
}

func TestListFilters(t *testing.T) {
	for _, tc := range []struct {
		filter listFilter
		ids    string
	}{
		{listFilter{running: true}, "db-prod"},
		{listFilter{favorites: true}, "db-prod"},
		{listFilter{kind: "cloudflared"}, "web"},
		{listFilter{profile: "work"}, "db-prod"},
		{listFilter{running: true, kind: "cloudflared"}, ""},
	} {
		var ids []string
		for _, view := range filterTunnels(fixtureTunnels(), tc.filter) {
			ids = append(ids, view.Tunnel.ID)
		}
		if got := strings.Join(ids, ","); got != tc.ids {
			t.Errorf("%+v: %q, want %q", tc.filter, got, tc.ids)
		}
	}
}

func TestJSONErrorsAndNonTTY(t *testing.T) {
	var out, diagnostics bytes.Buffer
	a := newApp(IO{In: strings.NewReader(""), Out: &out, Err: &diagnostics}, Options{})
	a.json = true
	a.writeError(&tunnelError{err: api.NewError(api.NeedsSecret, "secret missing"), tunnel: "web", hint: "dtt secret set token"})
	var got struct {
		Version int `json:"apiVersion"`
		Error   struct{ Code, Message, Tunnel, Hint string }
	}
	if err := json.Unmarshal(diagnostics.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != api.Version || got.Error.Code != "needs_secret" || got.Error.Tunnel != "web" || got.Error.Hint == "" || out.Len() != 0 {
		t.Fatalf("JSON error = %+v; stdout = %q", got, &out)
	}
	if a.canPrompt() || IsTerminal(&out) || a.color("31", "text") != "text" {
		t.Fatal("non-TTY must neither prompt nor color")
	}
	a.noInput = true
	if a.canPrompt() {
		t.Fatal("--no-input must not prompt")
	}
}

func TestEndpointPort(t *testing.T) {
	for address, want := range map[string]int{"5432": 5432, "127.0.0.1:5432": 5432, "[::1]:8080": 8080, "https://example.com": 443, "http://example.com:3000/a": 3000} {
		if got, err := endpointPort(address); err != nil || got != want {
			t.Errorf("%s: %d %v", address, got, err)
		}
	}
	if _, err := endpointPort("example.com"); err == nil {
		t.Fatal("endpoint without a port accepted")
	}
}
