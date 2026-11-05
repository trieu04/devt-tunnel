package cli

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(doctorCommand) }

func doctorCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Short:   "Check tools, keychain and daemon health",
		Long:    "Check daemon connectivity, tool paths and versions, OS keychain availability, config errors, SSH config warnings and whether dtt is on the shell PATH. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version. Missing tools are essential only when a saved tunnel needs them; SSH config and shell PATH issues are warnings.",
		Example: "  dtt doctor\n  dtt doctor --json --no-daemon-start",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var status api.DaemonStatus
			if err := a.call(cmd.Context(), api.DaemonStatusMethod, api.Empty{}, &status); err != nil {
				return err
			}
			var overview api.Overview
			if err := a.call(cmd.Context(), "config.overview", api.Empty{}, &overview); err != nil {
				return err
			}
			tools := []api.ToolInfo{}
			if err := a.call(cmd.Context(), "tool.list", api.Empty{}, &tools); err != nil {
				return err
			}
			var hosts api.SSHHostList
			if err := a.call(cmd.Context(), "ssh.hosts", api.SSHParams{ProfileID: overview.ActiveProfile}, &hosts); err != nil {
				return err
			}
			findings := doctorFindings(status, overview, tools, hosts)
			if path, err := exec.LookPath("dtt"); err != nil {
				findings = append(findings, checks.Finding{Severity: checks.Warning, Message: "dtt is not on the shell PATH", Field: "path"})
			} else {
				findings = append(findings, checks.Finding{Severity: checks.OK, Message: "dtt found at " + path, Field: "path"})
			}
			var err error
			if a.json {
				err = a.writeJSON(map[string]any{
					"daemon": status, "tools": tools, "ssh": hosts, "findings": findings,
					"keychain": map[string]any{"name": overview.KeychainName, "available": overview.KeychainAvailable},
				})
			} else {
				err = a.writeFindings(findings)
			}
			if err != nil {
				return err
			}
			for _, finding := range findings {
				if finding.Severity == checks.Error {
					return errors.New("essential health checks failed")
				}
			}
			return nil
		},
	}
}

func doctorFindings(status api.DaemonStatus, overview api.Overview, tools []api.ToolInfo, hosts api.SSHHostList) []checks.Finding {
	findings := []checks.Finding{{Severity: checks.OK, Message: fmt.Sprintf("daemon %s (API %d), pid %d", status.DaemonVersion, status.APIVersion, status.PID), Field: "daemon"}}
	if overview.ConfigError != "" {
		findings = append(findings, checks.Finding{Severity: checks.Error, Message: overview.ConfigError, Field: "config"})
	}
	severity, message := checks.OK, overview.KeychainName+" is available"
	if !overview.KeychainAvailable {
		severity, message = checks.Error, "the OS keychain is unavailable"
	}
	findings = append(findings, checks.Finding{Severity: severity, Message: message, Field: "keychain"})
	required := map[string]bool{}
	for _, view := range overview.Tunnels {
		switch view.Tunnel.Type {
		case model.TypeSSH, model.TypeNgrok, model.TypeCloudflared:
			required[string(view.Tunnel.Type)] = true
		}
	}
	for _, tool := range tools {
		severity, message := checks.OK, fmt.Sprintf("%s: %s (%s)", tool.Name, tool.Path, tool.Version)
		if tool.Error != "" || tool.Path == "" {
			severity = checks.Warning
			if required[tool.Name] {
				severity = checks.Error
			}
			message = fmt.Sprintf("%s: %s", tool.Name, tool.Error)
			if tool.Error == "" {
				message = tool.Name + " was not found"
			}
		}
		findings = append(findings, checks.Finding{Severity: severity, Message: message, Field: "tools." + tool.Name})
	}
	for _, warning := range hosts.Warnings {
		findings = append(findings, checks.Finding{Severity: checks.Warning, Message: "SSH config: " + warning, Field: "ssh"})
	}
	return findings
}
