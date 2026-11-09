// Package cli implements the headless devt-tunnel command line.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/platform"
	"golang.org/x/term"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Options struct {
	Version     string
	SecretStore func() secret.Store
}

type app struct {
	io      IO
	options Options

	json, quiet, noColor, noDaemonStart, noInput bool
	timeout                                      time.Duration
	conn                                         *client.Client
	executing                                    bool
}

// Each group registers constructors in its own file's init; adding a group never
// requires changing root.go. Constructors must return fresh commands on each run.
var commandGroups []func(*app) *cobra.Command

func register(makeCommand func(*app) *cobra.Command) {
	commandGroups = append(commandGroups, makeCommand)
}

func Run(ctx context.Context, args []string, streams IO, options Options) int {
	a := newApp(streams, options)
	defer a.close()
	root := newRoot(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil && !a.executing {
		err = &UsageError{Message: err.Error()}
		// A bad flag can stop Cobra before a later --json is parsed.
		a.json = requestedJSON(args)
	}
	if err != nil {
		a.writeError(err)
	}
	return ExitCode(err)
}

func requestedJSON(args []string) bool {
	var enabled bool
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			enabled = true
		} else if value, ok := strings.CutPrefix(arg, "--json="); ok {
			enabled, _ = strconv.ParseBool(value)
		}
	}
	return enabled
}

func newApp(streams IO, options Options) *app {
	if streams.In == nil {
		streams.In = os.Stdin
	}
	if streams.Out == nil {
		streams.Out = io.Discard
	}
	if streams.Err == nil {
		streams.Err = io.Discard
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	return &app{io: streams, options: options, timeout: 10 * time.Second}
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "devt-tunnel",
		Short:         "Supervise and inspect development tunnels",
		Long:          "Manage development tunnels through the per-user daemon. Use traditional ssh, ngrok and cloudflared commands under one supervisor.",
		Example:       "  devt-tunnel ls\n  devt-tunnel show db\n  devt-tunnel daemon status",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.executing = true
			if a.timeout <= 0 {
				return usageError("--timeout must be positive")
			}
			return nil
		},
	}
	root.SetIn(a.io.In)
	root.SetOut(a.io.Out)
	root.SetErr(a.io.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &UsageError{Message: err.Error()} })
	root.CompletionOptions.DisableDefaultCmd = true
	flags := root.PersistentFlags()
	flags.BoolVar(&a.json, "json", false, "Write versioned JSON results and errors")
	flags.BoolVarP(&a.quiet, "quiet", "q", false, "Suppress informational diagnostics")
	flags.BoolVar(&a.noColor, "no-color", false, "Disable color (also NO_COLOR)")
	flags.DurationVar(&a.timeout, "timeout", a.timeout, "Daemon connection and request timeout")
	flags.BoolVar(&a.noDaemonStart, "no-daemon-start", false, "Do not start the daemon automatically")
	flags.BoolVar(&a.noInput, "no-input", false, "Never prompt for input")
	for _, makeCommand := range commandGroups {
		root.AddCommand(makeCommand(a))
	}
	root.SetHelpCommand(&cobra.Command{
		Use:     "help [command...]",
		Short:   "Show help for a command",
		Long:    "Show the usage, flags and examples for any devt-tunnel command.",
		Example: "  devt-tunnel help ls\n  devt-tunnel help daemon run",
		RunE: func(_ *cobra.Command, args []string) error {
			cmd, rest, err := root.Find(args)
			if err != nil {
				return &UsageError{Message: err.Error()}
			}
			if len(rest) != 0 {
				return usageError("unknown help topic %q", rest[0])
			}
			return cmd.Help()
		},
	})
	attachCompletions(root, a)
	return root
}

func (a *app) info() api.ClientInfo {
	return api.ClientInfo{Kind: "cli", Name: "devt-tunnel", Version: a.options.Version}
}

func (a *app) client(ctx context.Context) (*client.Client, error) {
	if a.conn != nil {
		return a.conn, nil
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	socket, err := platform.ControlSocket()
	if err != nil {
		return nil, &daemonError{err}
	}
	if a.noDaemonStart {
		a.conn, err = client.Dial(ctx, socket, a.info())
	} else {
		var path string
		path, err = os.Executable()
		if err == nil {
			a.conn, err = client.Ensure(ctx, client.EnsureOptions{Socket: socket, Info: a.info(), DaemonPath: path})
		}
	}
	if err != nil {
		return nil, &daemonError{err}
	}
	return a.conn, nil
}

func (a *app) call(ctx context.Context, method string, params, result any) error {
	c, err := a.client(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	return c.Call(ctx, method, params, result)
}

func (a *app) close() {
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

func IsTerminal(stream any) bool {
	fd, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(fd.Fd()))
}

func (a *app) canPrompt() bool { return !a.noInput && IsTerminal(a.io.In) }

func (a *app) color(code, text string) string {
	_, noColor := os.LookupEnv("NO_COLOR")
	if a.noColor || noColor || a.json || !IsTerminal(a.io.Out) {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func usageError(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}
