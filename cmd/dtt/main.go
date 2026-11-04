package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/daemon"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

var version = "dev"
var secretStore = func() secret.Store { return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, diagnostics io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(out, "dtt %s (API %d)\n", version, api.Version)
		return 0
	}
	if len(args) < 2 || args[0] != "daemon" {
		fmt.Fprintln(diagnostics, "usage: dtt version | daemon run|status|stop|restart [--timeout D] [--json]")
		return 2
	}
	command := args[1]
	if command != "run" && command != "status" && command != "stop" && command != "restart" {
		fmt.Fprintln(diagnostics, "unknown daemon command")
		return 2
	}
	flags := flag.NewFlagSet("daemon "+command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	timeout := flags.Duration("timeout", 10*time.Second, "daemon connection timeout")
	jsonOutput := flags.Bool("json", false, "write structured output")
	idle := daemon.DefaultIdleExit
	if command == "run" {
		flags.DurationVar(&idle, "idle-exit", idle, "exit after idle duration; 0 disables idle exit")
	}
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 || idle < 0 {
		fmt.Fprintln(diagnostics, "invalid daemon arguments")
		return 2
	}
	report := func(err error) int {
		if *jsonOutput {
			_ = json.NewEncoder(diagnostics).Encode(map[string]any{"apiVersion": api.Version, "error": map[string]any{"code": "daemon", "message": err.Error()}})
		} else {
			fmt.Fprintln(diagnostics, err)
		}
		return 7
	}
	socket, err := platform.ControlSocket()
	if err != nil {
		return report(err)
	}
	if command == "run" {
		err = daemon.Run(context.Background(), daemon.Options{Socket: socket, Version: version, IdleExit: idle, Engine: engine.Options{Secrets: secretStore()}})
		if err != nil {
			return report(err)
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	info := api.ClientInfo{Kind: "cli", Name: "dtt", Version: version}
	c, err := client.DialManagement(ctx, socket, info)
	if err != nil && command != "restart" {
		return report(err)
	}
	if c != nil {
		defer c.Close()
		if command == "status" {
			var status api.DaemonStatus
			if err = c.Call(ctx, api.DaemonStatusMethod, api.Empty{}, &status); err != nil {
				return report(err)
			}
			if *jsonOutput {
				_ = json.NewEncoder(out).Encode(status)
			} else {
				fmt.Fprintf(out, "daemon %s (API %d), pid %d, active=%t, clients=%d\n", status.DaemonVersion, status.APIVersion, status.PID, status.Active, status.Connections)
				for _, tunnel := range status.Tunnels {
					fmt.Fprintf(out, "%s\t%s\tpid=%d\t%s\n", tunnel.ID, tunnel.State, tunnel.PID, tunnel.Address)
				}
			}
			return 0
		}
		fmt.Fprintln(diagnostics, "Stopping daemon and all tunnels.")
		if err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{}, nil); err != nil {
			return report(err)
		}
		select {
		case <-c.Done():
		case <-ctx.Done():
			return report(ctx.Err())
		}
	}
	if command == "restart" {
		path, err := os.Executable()
		if err != nil {
			return report(err)
		}
		c, err = client.Ensure(ctx, client.EnsureOptions{Socket: socket, Info: info, DaemonPath: path})
		if err != nil {
			return report(err)
		}
		defer c.Close()
	}
	if *jsonOutput {
		_ = json.NewEncoder(out).Encode(map[string]any{"apiVersion": api.Version, "status": command + " complete"})
	} else {
		fmt.Fprintln(out, "daemon", command, "complete")
	}
	return 0
}
