package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
)

func init() {
	register(showCommand)
	register(addrCommand)
	register(checkCommand)
}

func showCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "show <t>",
		Short:   "Inspect a tunnel definition and runtime state",
		Long:    "Show a tunnel's definition, state, endpoints, exact preview command, missing secrets and editor findings. Replaces ps aux | grep ssh and lsof -i :PORT. Refer to a tunnel by ID, name or unique ID prefix.",
		Example: "  dtt show db\n  dtt show 'Production DB' --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			findings := []checks.Finding{}
			if err = a.call(cmd.Context(), "tunnel.findings", api.IDParams{ID: view.Tunnel.ID}, &findings); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{
					"tunnel": view.Tunnel, "status": view.Status, "endpoints": view.Endpoints,
					"preview": view.Preview, "missingSecrets": view.MissingSecrets, "findings": findings,
				})
			}
			definition, err := json.MarshalIndent(view.Tunnel, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintf(a.io.Out, "%s (%s)\nState: %s\nPID: %d\n", view.Tunnel.Name, view.Tunnel.ID, view.Status.State, view.Status.PID)
			if view.Status.Error != "" {
				fmt.Fprintln(a.io.Out, "Error:", view.Status.Error)
			}
			for _, endpoint := range view.Endpoints {
				fmt.Fprintf(a.io.Out, "Endpoint (%s): %s", endpoint.Kind, endpoint.Listen)
				if endpoint.Target != "" {
					fmt.Fprintf(a.io.Out, " -> %s", endpoint.Target)
				}
				fmt.Fprintln(a.io.Out)
			}
			fmt.Fprintf(a.io.Out, "Command: %s\nMissing secrets: %s\nDefinition:\n%s\nFindings:\n", view.Preview, strings.Join(view.MissingSecrets, ", "), definition)
			return a.writeFindings(findings)
		},
	}
}

func addrCommand(a *app) *cobra.Command {
	var port, all bool
	cmd := &cobra.Command{
		Use:     "addr <t>",
		Short:   "Print a tunnel's address",
		Long:    "Print the first endpoint without labels, suitable for shell substitution. --all prints every endpoint; --port prints just the first endpoint's port. Replaces extracting a forward's listen address from ssh -L or process arguments.",
		Example: "  dtt addr db\n  psql \"host=127.0.0.1 port=$(dtt addr db --port) dbname=app\"\n  dtt addr db --all",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if port && all {
				return usageError("--port and --all cannot be used together")
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if len(view.Endpoints) == 0 || !all && view.Endpoints[0].Listen == "" {
				return &tunnelError{err: errors.New("tunnel has no endpoint yet"), tunnel: view.Tunnel.ID}
			}
			if port {
				n, err := endpointPort(view.Endpoints[0].Listen)
				if err != nil {
					return err
				}
				if a.json {
					return a.writeJSON(map[string]any{"id": view.Tunnel.ID, "port": n})
				}
				_, err = fmt.Fprintln(a.io.Out, n)
				return err
			}
			addresses := []string{view.Endpoints[0].Listen}
			if all {
				addresses = addresses[:0]
				for _, endpoint := range view.Endpoints {
					if endpoint.Listen != "" {
						addresses = append(addresses, endpoint.Listen)
					}
				}
			}
			if len(addresses) == 0 {
				return &tunnelError{err: errors.New("tunnel has no endpoint yet"), tunnel: view.Tunnel.ID}
			}
			if a.json {
				return a.writeJSON(map[string]any{"id": view.Tunnel.ID, "addresses": addresses})
			}
			_, err = fmt.Fprintln(a.io.Out, strings.Join(addresses, "\n"))
			return err
		},
	}
	cmd.Flags().BoolVar(&port, "port", false, "Print only the first endpoint's port")
	cmd.Flags().BoolVar(&all, "all", false, "Print every endpoint")
	return cmd
}

func endpointPort(address string) (int, error) {
	value := address
	if strings.Contains(address, "://") {
		u, err := url.Parse(address)
		if err != nil {
			return 0, fmt.Errorf("invalid endpoint %q: %w", address, err)
		}
		value = u.Port()
		if value == "" {
			switch u.Scheme {
			case "http":
				value = "80"
			case "https":
				value = "443"
			}
		}
	} else if _, p, err := net.SplitHostPort(address); err == nil {
		value = p
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("endpoint %q has no port", address)
	}
	return port, nil
}

func checkCommand(a *app) *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:     "check <t>",
		Short:   "Run editor checks for a saved tunnel",
		Long:    "Run the same binary, secret, SSH option and port checks as the editor. Errors fail the command; --strict also fails on warnings. Replaces manually checking binaries with command -v and ports with lsof -i :PORT.",
		Example: "  dtt check db\n  dtt check db --strict --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			findings := []checks.Finding{}
			if err = a.call(cmd.Context(), "tunnel.findings", api.IDParams{ID: view.Tunnel.ID}, &findings); err != nil {
				return err
			}
			if a.json {
				err = a.writeJSON(map[string]any{"id": view.Tunnel.ID, "findings": findings})
			} else {
				err = a.writeFindings(findings)
			}
			if err != nil {
				return err
			}
			for _, finding := range findings {
				if finding.Severity == checks.Error || strict && finding.Severity == checks.Warning {
					return &tunnelError{err: errors.New("tunnel checks failed"), tunnel: view.Tunnel.ID}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail on warnings as well as errors")
	return cmd
}
