package cli

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func init() { register(orphansCommand) }

func orphansCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "orphans", Short: "List tunnel processes left after a crash",
		Long:    "List tunnel processes recovered from crash records. The daemon checks OS process identities to protect against PID reuse. Replaces searching ps output for abandoned tunnel processes.",
		Example: "  dtt orphans\n  dtt orphans --json\n  dtt orphans stop 12345", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			records := []supervisor.Record{}
			if err := a.call(cmd.Context(), "orphan.list", api.Empty{}, &records); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"orphans": records})
			}
			rows := make([][]string, 0, len(records))
			for _, record := range records {
				rows = append(rows, []string{strconv.Itoa(record.PID), record.TunnelID, record.Identity})
			}
			return a.table([]string{"PID", "TUNNEL", "IDENTITY"}, rows)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use: "stop <pid>", Short: "Stop a recovered orphan process",
		Long:    "Stop a process listed by dtt orphans. The daemon verifies its recorded OS identity before stopping it, unlike an unconditional kill PID.",
		Example: "  dtt orphans stop 12345\n  dtt orphans stop 12345 --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := strconv.Atoi(args[0])
			if err != nil || pid <= 0 {
				return usageError("pid must be a positive integer")
			}
			if err := a.call(cmd.Context(), "orphan.stop", api.OrphanParams{PID: pid}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"pid": pid, "status": "stopped"})
			}
			a.diagnostic("Orphan %d stopped.", pid)
			return nil
		},
	})
	return cmd
}
