package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/mcpserver"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func TestMCPConfig(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"mcp", "config"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var out, diagnostics bytes.Buffer
		if code := Run(context.Background(), args, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &diagnostics)
		}
		if mcpTemporaryExecutable(executable) && !strings.Contains(diagnostics.String(), "temporary") {
			t.Fatalf("temporary test executable needs a stderr warning: %s", &diagnostics)
		}
		data := out.String()
		if !jsonOutput {
			prefix := "Claude Code:\n" + mcpAddCommand(executable) + "\n\nClient configuration:\n"
			if !strings.HasPrefix(data, prefix) {
				t.Fatalf("missing shell command: %s", data)
			}
			data = strings.TrimPrefix(data, prefix)
		}
		var object map[string]json.RawMessage
		decoder := json.NewDecoder(strings.NewReader(data))
		if err := decoder.Decode(&object); err != nil || len(object) != 1 || object["mcpServers"] == nil {
			t.Fatalf("paste-ready config must have only mcpServers, not an apiVersion envelope: %s (%v)", data, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("unexpected trailing output: %v", err)
		}
		var config mcpClientConfig
		if err := json.Unmarshal([]byte(data), &config); err != nil {
			t.Fatal(err)
		}
		server := config.Servers["devt-tunnel"]
		if len(config.Servers) != 1 || server.Command != executable || !filepath.IsAbs(server.Command) || !reflect.DeepEqual(server.Args, []string{"mcp", "serve"}) {
			t.Fatalf("unexpected configuration: %+v", config)
		}
	}
}

func TestMCPConfigShellQuoting(t *testing.T) {
	for _, path := range []string{"/Applications/Tunnel Tools/dtt", "/home/person's tools/dtt", `C:\Program Files\Tunnel Tools\dtt.exe`} {
		line := mcpAddCommand(path)
		args, err := command.SplitArgs(line, platform.Dialect())
		want := []string{"claude", "mcp", "add", "devt-tunnel", "--", path, "mcp", "serve"}
		if err != nil || !reflect.DeepEqual(args, want) {
			t.Errorf("shell-quoted command %q: got %q (%v), want %q", line, args, err, want)
		}
	}
}

func TestMCPTemporaryExecutable(t *testing.T) {
	for path, want := range map[string]bool{
		"/tmp/go-build123/b001/exe/dtt":   true,
		"/tmp/TestBuild/exe/cli.test":     true,
		"/tmp/TestBuild/exe/cli.test.exe": true,
		"/usr/local/bin/dtt":              false,
		"/tmp/installed/dtt":              false,
	} {
		if got := mcpTemporaryExecutable(filepath.FromSlash(path)); got != want {
			t.Errorf("%q: temporary=%t, want %t", path, got, want)
		}
	}
}

func TestMCPTools(t *testing.T) {
	t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	want, err := json.Marshal(mcpserver.Tools())
	if err != nil {
		t.Fatal(err)
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"mcp", "tools"}
		if jsonOutput {
			args = append(args, "--json")
		}
		var out, diagnostics bytes.Buffer
		if code := Run(context.Background(), args, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 || diagnostics.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &diagnostics)
		}
		data := out.String()
		if !jsonOutput {
			data = strings.TrimPrefix(data, "MCP tools (descriptions, schemas and annotations):\n")
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(data)); err != nil || !bytes.Equal(compact.Bytes(), want) {
			t.Fatalf("tools must be raw Tools() with no envelope: %s (%v)", &out, err)
		}
		var tools []*mcp.Tool
		if err := json.Unmarshal(compact.Bytes(), &tools); err != nil || len(tools) != 10 {
			t.Fatalf("tool array: len=%d err=%v", len(tools), err)
		}
		for _, tool := range tools {
			if tool.Name == "" || tool.Description == "" || tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil {
				t.Errorf("tool is missing review information: %+v", tool)
			}
		}
	}
}

// MCP success output is deliberately not the CLI JSON envelope. Keep the
// ordinary-object contract strict rather than accepting either shape everywhere.
func TestMCPJSONExceptions(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if code := Run(context.Background(), []string{"docs", "--json"}, IO{Out: &out, Err: &diagnostics}, Options{}); code != 0 {
		t.Fatalf("ordinary CLI JSON: code=%d stderr=%s", code, &diagnostics)
	}
	var ordinary struct {
		Version int `json:"apiVersion"`
	}
	if err := json.Unmarshal(out.Bytes(), &ordinary); err != nil || ordinary.Version != api.Version {
		t.Fatalf("ordinary CLI output still requires apiVersion: %s (%v)", &out, err)
	}
	root := newRoot(newApp(IO{}, Options{}))
	for _, name := range []string{"config", "tools"} {
		cmd, _, err := root.Find([]string{"mcp", name})
		if err != nil || !strings.Contains(cmd.Long, "envelope") {
			t.Errorf("mcp %s must document its JSON envelope exception: %v", name, err)
		}
	}
}

func TestMCPServeStdoutPurity(t *testing.T) {
	for _, jsonFlag := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "json-flag"}[jsonFlag], func(t *testing.T) {
			t.Setenv("DTT_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			serverIn, clientOut := io.Pipe()
			clientIn, serverOut := io.Pipe()
			defer serverIn.Close()
			defer clientOut.Close()
			defer clientIn.Close()
			defer serverOut.Close()
			var output, diagnostics bytes.Buffer
			done := make(chan int, 1)
			go func() {
				args := []string{"mcp", "serve", "--name", "stdio-test", "--no-daemon-start"}
				if jsonFlag {
					args = append(args, "--json")
				}
				code := Run(ctx, args, IO{In: serverIn, Out: io.MultiWriter(&output, serverOut), Err: &diagnostics}, Options{Version: "test"})
				_ = serverOut.Close()
				done <- code
			}()
			client := mcp.NewClient(&mcp.Implementation{Name: "cli-test", Version: "test"}, nil)
			session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
			if err != nil {
				t.Fatal(err)
			}
			tools, err := session.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 10 {
				t.Fatalf("tools/list: %+v (%v)", tools, err)
			}
			// A tool-level daemon failure must also remain a JSON-RPC frame,
			// never a CLI banner, table or success envelope on stdout.
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "tunnel_list", Arguments: map[string]any{}})
			if err != nil || !result.IsError {
				t.Fatalf("absent daemon must return an MCP tool error: %+v (%v)", result, err)
			}
			_ = session.Close()
			select {
			case code := <-done:
				if code != 0 || diagnostics.Len() != 0 {
					t.Fatalf("serve: code=%d stderr=%s", code, &diagnostics)
				}
			case <-ctx.Done():
				t.Fatal("serve did not exit after disconnect")
			}
			scanner := bufio.NewScanner(&output)
			scanner.Buffer(make([]byte, 4096), 1024*1024)
			frames := 0
			for scanner.Scan() {
				var frame map[string]json.RawMessage
				if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil || string(frame["jsonrpc"]) != `"2.0"` || frame["apiVersion"] != nil || (frame["id"] == nil && frame["method"] == nil) {
					t.Fatalf("non-JSON-RPC stdout: %s (%v)", scanner.Bytes(), err)
				}
				frames++
			}
			if err := scanner.Err(); err != nil || frames < 3 {
				t.Fatalf("expected initialize/list/call frames: count=%d err=%v", frames, err)
			}
		})
	}
}

func TestMCPUsageErrorsStayOnStderr(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "config", "extra", "--json"},
		{"mcp", "tools", "extra", "--json"},
		{"mcp", "serve", "--unknown", "--json"},
	} {
		var out, diagnostics bytes.Buffer
		if code := Run(context.Background(), args, IO{Out: &out, Err: &diagnostics}, Options{}); code != 2 || out.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &diagnostics)
		}
		var envelope struct {
			Version int `json:"apiVersion"`
			Error   struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(diagnostics.Bytes(), &envelope); err != nil || envelope.Version != api.Version || envelope.Error.Code != "usage" {
			t.Errorf("CLI errors retain their normal envelope: %s (%v)", &diagnostics, err)
		}
	}
}
