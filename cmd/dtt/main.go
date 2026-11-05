package main

import (
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/trieu04/devt-tunnel/internal/cli"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

var version = "dev"
var secretStore = func() secret.Store { return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, diagnostics io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return cli.Run(ctx, args, cli.IO{In: os.Stdin, Out: out, Err: diagnostics}, cli.Options{Version: version, SecretStore: secretStore})
}
