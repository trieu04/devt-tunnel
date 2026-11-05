package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func init() { register(logsCommand) }

func logsCommand(a *app) *cobra.Command {
	var follow bool
	var count int
	var since time.Duration
	cmd := &cobra.Command{
		Use:     "logs <t>",
		Short:   "Read or follow retained tunnel output",
		Long:    "Print retained tunnel output, like tail -n N or tail -f on a log file. Follow polls until Ctrl-C. With --json each batch is a versioned JSON object on its own line. The current daemon does not timestamp log lines, so --since is unavailable until it does.",
		Example: "  dtt logs db -n 50\n  dtt logs db -f\n  dtt logs db --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if count < 0 || since < 0 {
				return usageError("-n and --since must not be negative")
			}
			if since > 0 {
				return usageError("--since is unavailable: daemon log lines have no timestamps")
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.readLogs(cmd.Context(), view.Tunnel.ID, count, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow new output until interrupted")
	cmd.Flags().IntVarP(&count, "lines", "n", 100, "Number of retained lines to print")
	cmd.Flags().DurationVar(&since, "since", 0, "Only lines from this duration ago (requires daemon timestamps)")
	return cmd
}

func (a *app) readLogs(ctx context.Context, id string, count int, follow bool) error {
	var batch api.LogsResult
	if err := a.call(ctx, "logs.since", api.LogsParams{ID: id}, &batch); err != nil {
		return err
	}
	if len(batch.Lines) > count {
		batch.Lines = batch.Lines[len(batch.Lines)-count:]
	}
	if err := a.writeLogs(id, batch); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var next api.LogsResult
			if err := a.call(ctx, "logs.since", api.LogsParams{ID: id, Sequence: batch.Sequence}, &next); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			batch = next
			if len(batch.Lines) != 0 {
				if err := a.writeLogs(id, batch); err != nil {
					return err
				}
			}
		}
	}
}

func (a *app) writeLogs(id string, batch api.LogsResult) error {
	if batch.Lines == nil {
		batch.Lines = []supervisor.LogLine{}
	}
	if a.json {
		return a.writeJSON(map[string]any{"id": id, "sequence": batch.Sequence, "lines": batch.Lines})
	}
	for _, line := range batch.Lines {
		if _, err := fmt.Fprintln(a.io.Out, line.Line); err != nil {
			return err
		}
	}
	return nil
}
