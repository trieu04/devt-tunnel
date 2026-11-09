package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() { register(editCommand) }

func editCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "edit <t>",
		Short:   "Edit a tunnel as a TOML fragment",
		Long:    "Write one tunnel to a private temporary TOML file, open $VISUAL or $EDITOR (falling back to vi, or notepad on Windows), validate through the daemon and save. The ID must not change. Invalid TOML or definitions can be reopened on a TTY. Redirected input requires an explicitly configured VISUAL or EDITOR. Replaces hand-editing a tunnel block in tunnels.toml.",
		Example: "  devt-tunnel edit db\n  EDITOR='code --wait' devt-tunnel edit db\n  EDITOR=./update-tunnel devt-tunnel edit db --no-input --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			editor, explicit := defineEditor()
			if !explicit && !a.canPrompt() {
				return usageError("edit requires a TTY unless VISUAL or EDITOR is explicitly set")
			}
			argv, err := command.SplitArgs(editor, platform.Dialect())
			if err != nil || len(argv) == 0 {
				return usageError("invalid VISUAL or EDITOR command")
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var fragment bytes.Buffer
			fragment.WriteString("# Edit one tunnel. Its id is permanent and must not change.\n")
			if err := toml.NewEncoder(&fragment).Encode(view.Tunnel); err != nil {
				return err
			}
			file, err := os.CreateTemp("", "devt-tunnel-edit-*.toml")
			if err != nil {
				return err
			}
			defer os.Remove(file.Name())
			if _, err := file.Write(fragment.Bytes()); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
			for {
				process := exec.CommandContext(cmd.Context(), argv[0], append(argv[1:], file.Name())...)
				process.Stdin, process.Stderr = a.io.In, a.io.Err
				process.Stdout = a.io.Out
				if a.json {
					// An editor must not corrupt the versioned JSON result stream.
					process.Stdout = a.io.Err
				}
				if err := process.Run(); err != nil {
					return fmt.Errorf("editor: %w", err)
				}
				t, parseErr := defineReadFragment(file.Name(), view.Tunnel.ID)
				if parseErr != nil {
					reopen, err := a.defineReopen(parseErr)
					if err != nil {
						return err
					}
					if reopen {
						continue
					}
					return parseErr
				}
				result, err := a.evaluateDefinition(cmd.Context(), t, nil)
				if err != nil {
					return err
				}
				if validationErr := definitionError(result); validationErr != nil {
					if a.canPrompt() {
						if !a.json {
							if err := a.writeFindings(result.Findings); err != nil {
								return err
							}
						}
						reopen, err := a.defineReopen(validationErr)
						if err != nil {
							return err
						}
						if reopen {
							continue
						}
					}
				}
				return a.saveDefinition(cmd.Context(), t, defineOptions{}, result)
			}
		},
	}
}

func defineEditor() (string, bool) {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := os.Getenv(name); strings.TrimSpace(value) != "" {
			return value, true
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad", false
	}
	return "vi", false
}

func defineReadFragment(path, id string) (model.Tunnel, error) {
	var t model.Tunnel
	data, err := os.ReadFile(path)
	if err != nil {
		return t, err
	}
	metadata, err := toml.Decode(string(data), &t)
	if err != nil {
		return t, usageError("invalid tunnel TOML: %v", err)
	}
	if keys := metadata.Undecoded(); len(keys) > 0 {
		return t, usageError("unknown tunnel TOML key %q", keys[0])
	}
	if t.ID != id {
		return t, usageError("tunnel id must remain %q; use dup to create a new tunnel", id)
	}
	return t, nil
}

func (a *app) defineReopen(problem error) (bool, error) {
	if !a.canPrompt() {
		return false, nil
	}
	if _, err := fmt.Fprintln(a.io.Err, problem); err != nil {
		return false, err
	}
	return a.defineConfirm("Reopen editor? [y/N] ")
}
