package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
)

func init() { register(hostsCommand) }

func hostsCommand(a *app) *cobra.Command {
	var alias, profile string
	cmd := &cobra.Command{
		Use:     "hosts",
		Short:   "List SSH config aliases or resolve one",
		Long:    "List literal SSH aliases and their source files. --profile selects a profile's SSH config. --resolve shows effective connection settings, equivalent to ssh -G ALIAS (and ssh -F CONFIG -G ALIAS for a profile-specific config).",
		Example: "  dtt hosts\n  dtt hosts --profile work\n  dtt hosts --resolve server1 --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := api.SSHParams{Alias: alias, ProfileID: profile}
			if alias != "" {
				var host sshconfig.Resolved
				if err := a.call(cmd.Context(), "ssh.resolve", params, &host); err != nil {
					return err
				}
				if a.json {
					return a.writeJSON(map[string]any{"alias": alias, "host": map[string]any{
						"hostName": host.HostName, "user": host.User, "port": host.Port,
						"proxyJump": host.ProxyJump, "identityFiles": host.IdentityFiles,
					}})
				}
				return a.table([]string{"SETTING", "VALUE"}, [][]string{
					{"HostName", host.HostName}, {"User", host.User}, {"Port", host.Port},
					{"ProxyJump", host.ProxyJump}, {"IdentityFiles", strings.Join(host.IdentityFiles, ", ")},
				})
			}
			var list api.SSHHostList
			if err := a.call(cmd.Context(), "ssh.hosts", params, &list); err != nil {
				return err
			}
			if list.Hosts == nil {
				list.Hosts = []sshconfig.Host{}
			}
			if list.Warnings == nil {
				list.Warnings = []string{}
			}
			if a.json {
				return a.writeJSON(map[string]any{"configPath": list.ConfigPath, "hosts": list.Hosts, "warnings": list.Warnings})
			}
			for _, warning := range list.Warnings {
				a.diagnostic("warning: %s", warning)
			}
			rows := make([][]string, 0, len(list.Hosts))
			for _, host := range list.Hosts {
				rows = append(rows, []string{host.Alias, host.Source})
			}
			return a.table([]string{"ALIAS", "SOURCE"}, rows)
		},
	}
	cmd.Flags().StringVar(&alias, "resolve", "", "Resolve an alias through ssh -G")
	cmd.Flags().StringVar(&profile, "profile", "", "Use this profile ID's SSH config")
	return cmd
}
