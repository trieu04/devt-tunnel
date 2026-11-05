package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/mcpserver"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() { register(mcpCommand) }

func mcpCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "mcp", Short: "Connect agents through the policy-controlled MCP adapter",
		Long:    "Serve MCP over stdio, print paste-ready client configuration, or review tool descriptions and schemas offline. The adapter uses the daemon's agent policy, approvals and leases; it does not grant agents person-client privileges.",
		Example: "  dtt mcp config\n  dtt mcp tools --json\n  dtt mcp serve --name coding-agent",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(mcpServeCommand(a), mcpConfigCommand(a), mcpToolsCommand(a))
	return cmd
}

func mcpServeCommand(a *app) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use: "serve", Short: "Serve MCP JSON-RPC on stdin and stdout",
		Long:    "Run the stdio-only MCP adapter until the client disconnects or the context is canceled. Only JSON-RPC is written to stdout, even with --json; CLI errors and diagnostics go to stderr. The daemon starts on demand unless --no-daemon-start is set. --name overrides the MCP client's label in the app and audit log. Agents cannot approve requests, answer SSH prompts or set secrets.",
		Example: "  dtt mcp serve\n  dtt mcp serve --name coding-agent\n  dtt mcp serve --no-daemon-start",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			socket, err := platform.ControlSocket()
			if err != nil {
				return &daemonError{err}
			}
			executable, err := mcpExecutable()
			if err != nil {
				return err
			}
			adapter := mcpserver.New(mcpserver.Options{Version: a.options.Version, Name: name, Socket: socket, DaemonPath: executable, NoStart: a.noDaemonStart})
			defer adapter.Close()
			// The SDK needs closers to unblock pipe reads on cancellation. Preserve
			// supplied read closers, but never let it close the caller's stdout.
			reader, ok := a.io.In.(io.ReadCloser)
			if !ok {
				reader = io.NopCloser(a.io.In)
			}
			return adapter.Server().Run(cmd.Context(), &mcp.IOTransport{Reader: reader, Writer: mcpWriter{a.io.Out}})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Client label shown in the app and audit log (default: MCP client name)")
	return cmd
}

type mcpWriter struct{ io.Writer }

func (mcpWriter) Close() error { return nil }

type mcpClientConfig struct {
	Servers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func mcpExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func mcpTemporaryExecutable(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(part, "go-build") {
			return true
		}
	}
	return strings.HasSuffix(path, ".test") || strings.HasSuffix(path, ".test.exe")
}

func mcpAddCommand(executable string) string {
	return defineJoinArgs([]string{"claude", "mcp", "add", "devt-tunnel", "--", executable, "mcp", "serve"})
}

func mcpConfigCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "config", Short: "Print client configuration with this executable's absolute path",
		Long:    "Print a shell-quoted Claude Code registration command and an mcpServers JSON configuration using this executable's absolute, symlink-resolved path. No daemon is needed. With --json, stdout is only the paste-ready configuration object {\"mcpServers\":{\"devt-tunnel\":{\"command\":\"/absolute/dtt\",\"args\":[\"mcp\",\"serve\"]}}}, deliberately without the usual apiVersion envelope. Temporary go run/test executable paths produce a stderr warning; install a stable binary before registering it.",
		Example: "  dtt mcp config\n  dtt mcp config --json > mcp-config.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			executable, err := mcpExecutable()
			if err != nil {
				return err
			}
			if mcpTemporaryExecutable(executable) {
				a.diagnostic("Warning: executable path %q is temporary; install dtt at a stable path and rerun mcp config before registering it.", executable)
			}
			if !a.json {
				if _, err := fmt.Fprintf(a.io.Out, "Claude Code:\n%s\n\nClient configuration:\n", mcpAddCommand(executable)); err != nil {
					return err
				}
			}
			config := mcpClientConfig{Servers: map[string]mcpServerConfig{"devt-tunnel": {Command: executable, Args: []string{"mcp", "serve"}}}}
			encoder := json.NewEncoder(a.io.Out)
			if !a.json {
				encoder.SetIndent("", "  ")
			}
			return encoder.Encode(config)
		},
	}
}

func mcpToolsCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "tools", Short: "Review MCP tool descriptions, schemas and annotations",
		Long:    "Print the tool definitions exported by the adapter, including descriptions, input/output schemas and safety annotations, without contacting the daemon. With --json, stdout is the raw MCP tool array, not a versioned CLI envelope. Review these definitions before enabling an agent client; the daemon remains the authority for policy and approval.",
		Example: "  dtt mcp tools\n  dtt mcp tools --json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			encoder := json.NewEncoder(a.io.Out)
			if !a.json {
				if _, err := fmt.Fprintln(a.io.Out, "MCP tools (descriptions, schemas and annotations):"); err != nil {
					return err
				}
				encoder.SetIndent("", "  ")
			}
			return encoder.Encode(mcpserver.Tools())
		},
	}
}
