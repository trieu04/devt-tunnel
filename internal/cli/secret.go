package cli

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"golang.org/x/term"
)

func init() { register(secretCommand) }

var secretCLIName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func secretCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "secret", Short: "Manage named credentials without displaying values",
		Long:    "Store and manage credentials in the daemon's OS keychain. Values are accepted only through a hidden terminal prompt or explicit --stdin, never through arguments, flags or environment variables. There is no reveal command; use the desktop app for authenticated reveal.",
		Example: "  devt-tunnel secret ls\n  devt-tunnel secret set ngrok-personal --kind ngrok", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(secretListCommand(a), secretSetCommand(a), secretRenameCommand(a), secretRemoveCommand(a))
	return cmd
}

func secretListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "ls", Short: "List credential metadata, never values",
		Long:    "List credential names, kinds, tunnels using them, last use and presence state. No secret values are returned. An unavailable OS keychain exits with code 3.",
		Example: "  devt-tunnel secret ls\n  devt-tunnel secret ls --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			views := []api.SecretView{}
			if err := a.call(cmd.Context(), "secret.list", api.Empty{}, &views); err != nil {
				return err
			}
			var overview api.Overview
			if err := a.call(cmd.Context(), "config.overview", api.Empty{}, &overview); err != nil {
				return err
			}
			var err error
			if a.json {
				err = a.writeJSON(map[string]any{"secrets": views})
			} else {
				rows := make([][]string, 0, len(views))
				for _, view := range views {
					rows = append(rows, []string{view.Name, string(view.Kind), strings.Join(view.UsedBy, ", "), view.LastUsed, view.State})
				}
				err = a.table([]string{"NAME", "KIND", "USED BY", "LAST USED", "STATE"}, rows)
			}
			if err != nil {
				return err
			}
			if !overview.KeychainAvailable {
				return api.NewError(api.KeyringUnavailable, "the OS keychain is unavailable")
			}
			for _, view := range views {
				if view.State == "unavailable" {
					return api.NewError(api.KeyringUnavailable, "the OS keychain is unavailable")
				}
			}
			return nil
		},
	}
}

func secretSetCommand(a *app) *cobra.Command {
	var kind string
	var stdin bool
	cmd := &cobra.Command{
		Use: "set <name>", Short: "Store a credential from a hidden prompt or stdin",
		Long:    "Store a credential of the specified kind. Read a hidden value from the terminal, or use --stdin to read all input with one trailing newline removed. Without a TTY, --stdin is required. Empty values are rejected. Waiting tunnel starts retry after storage. Values are never accepted in flags or positional arguments.",
		Example: "  devt-tunnel secret set ngrok-personal --kind ngrok\n  op read op://dev/ngrok/token | devt-tunnel secret set ngrok-personal --kind ngrok --stdin", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := secretValidateName(args[0]); err != nil {
				return err
			}
			switch secret.Kind(kind) {
			case secret.KindSSHPassphrase, secret.KindNgrok, secret.KindCloudflared, secret.KindEnv:
			default:
				return usageError("--kind must be ssh_passphrase, ngrok, cloudflared or env")
			}
			value, err := a.secretReadValue(stdin)
			if err != nil {
				return err
			}
			defer clear(value)
			if len(value) == 0 {
				return usageError("secret value must not be empty")
			}
			if len(value) > 2560 {
				return usageError("secret is too large for the OS credential store (maximum 2560 bytes)")
			}
			if err := a.call(cmd.Context(), "secret.set", api.SetSecretParams{Name: args[0], Value: string(value), Kind: secret.Kind(kind)}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"name": args[0], "kind": kind, "status": "stored"})
			}
			a.diagnostic("Secret %s stored.", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Credential kind: ssh_passphrase, ngrok, cloudflared or env (required)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read the value from stdin instead of a hidden terminal prompt")
	return cmd
}

func (a *app) secretReadValue(stdin bool) ([]byte, error) {
	if stdin {
		value, err := io.ReadAll(a.io.In)
		if err != nil {
			clear(value)
			return nil, fmt.Errorf("read secret from stdin: %w", err)
		}
		if len(value) > 0 && value[len(value)-1] == '\n' {
			value = value[:len(value)-1]
			if len(value) > 0 && value[len(value)-1] == '\r' {
				value = value[:len(value)-1]
			}
		}
		return value, nil
	}
	if !a.canPrompt() {
		return nil, api.NewError(api.NeedsUser, "secret input needs a terminal; supply --stdin to read a piped value")
	}
	fd := a.io.In.(interface{ Fd() uintptr }).Fd()
	if _, err := fmt.Fprint(a.io.Err, "Secret value: "); err != nil {
		return nil, err
	}
	value, err := term.ReadPassword(int(fd))
	fmt.Fprintln(a.io.Err)
	if err != nil {
		clear(value)
		return nil, fmt.Errorf("read hidden secret: %w", err)
	}
	return value, nil
}

func secretValidateName(name string) error {
	if !secretCLIName.MatchString(name) {
		return usageError("secret name may contain only letters, numbers, dot, underscore and hyphen")
	}
	return nil
}

func secretRenameCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "rename <from> <to>", Short: "Rename a credential and update its users",
		Long:    "Rename a credential in the OS keychain and update all saved tunnel references. Values are never displayed or returned.",
		Example: "  devt-tunnel secret rename ngrok-personal ngrok-work\n  devt-tunnel secret rename old-token new-token --json", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range args {
				if err := secretValidateName(name); err != nil {
					return err
				}
			}
			if args[0] == args[1] {
				return usageError("the new secret name must differ from the old name")
			}
			if err := a.call(cmd.Context(), "secret.rename", api.RenameSecretParams{From: args[0], To: args[1]}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"from": args[0], "to": args[1], "status": "renamed"})
			}
			a.diagnostic("Secret %s renamed to %s.", args[0], args[1])
			return nil
		},
	}
}

func secretRemoveCommand(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "rm <name>", Short: "Delete a credential from the OS keychain",
		Long:    "Delete a credential, keeping tunnel references so affected tunnels report a missing token. Ask for confirmation on a terminal; --yes is required without a TTY or with --no-input.",
		Example: "  devt-tunnel secret rm ngrok-personal\n  devt-tunnel secret rm ngrok-personal --yes --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := secretValidateName(args[0]); err != nil {
				return err
			}
			if !yes {
				if !a.canPrompt() {
					return api.NewError(api.NeedsUser, "deleting a secret needs confirmation; supply --yes without a terminal")
				}
				if _, err := fmt.Fprintf(a.io.Err, "Delete secret %s? [y/N] ", args[0]); err != nil {
					return err
				}
				line, err := bufio.NewReader(a.io.In).ReadString('\n')
				if err != nil && err != io.EOF {
					return fmt.Errorf("read confirmation: %w", err)
				}
				answer := strings.ToLower(strings.TrimSpace(line))
				if answer != "y" && answer != "yes" {
					if a.json {
						return a.writeJSON(map[string]any{"name": args[0], "status": "cancelled"})
					}
					a.diagnostic("Deletion cancelled.")
					return nil
				}
			}
			if err := a.call(cmd.Context(), "secret.delete", api.SecretNameParams{Name: args[0]}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"name": args[0], "status": "deleted"})
			}
			a.diagnostic("Secret %s deleted.", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm deletion without prompting")
	return cmd
}
