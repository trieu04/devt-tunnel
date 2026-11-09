package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func init() {
	register(importCommand)
	register(setCommand)
	register(dupCommand)
	register(rmCommand)
}

type defineOptions struct {
	profile                              string
	groups                               []string
	favorite, startOnLaunch, noReconnect bool
	start, dryRun                        bool
}

func (o *defineOptions) flags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&o.profile, "profile", "", "Put in an existing profile by ID, name or unique ID prefix")
	f.StringArrayVar(&o.groups, "group", nil, "Add to an existing group by ID, name or unique ID prefix (repeatable)")
	f.BoolVar(&o.favorite, "favorite", false, "Mark as a favorite")
	f.BoolVar(&o.startOnLaunch, "start-on-launch", false, "Start when the app launches")
	f.BoolVar(&o.noReconnect, "no-reconnect", false, "Disable automatic reconnection")
	f.BoolVar(&o.start, "start", false, "Save and start the tunnel")
	f.BoolVar(&o.dryRun, "dry-run", false, "Print the exact evaluated command and findings without saving or starting")
}

func (o defineOptions) apply(ctx context.Context, a *app, t *model.Tunnel) error {
	t.Favorite, t.StartOnLaunch = o.favorite, o.startOnLaunch
	if o.noReconnect {
		t.Reconnect = false
	}
	if o.profile != "" {
		p, err := a.resolveProfile(ctx, o.profile)
		if err != nil {
			return err
		}
		t.Profile = p.ID
	}
	for _, ref := range o.groups {
		g, err := a.resolveGroup(ctx, ref)
		if err != nil {
			return err
		}
		if !slices.Contains(t.Groups, g.ID) {
			t.Groups = append(t.Groups, g.ID)
		}
	}
	return nil
}

// evaluateDefinition also checks the stored model's structural rules: the daemon
// preview builder intentionally accepts incomplete editor drafts.
func (a *app) evaluateDefinition(ctx context.Context, t model.Tunnel, warnings []string) (api.DraftResult, error) {
	var result api.DraftResult
	if err := a.call(ctx, "tunnel.evaluateDraft", api.TunnelParams{Tunnel: t}, &result); err != nil {
		return result, err
	}
	for _, warning := range warnings {
		result.Findings = append(result.Findings, checks.Finding{Severity: checks.Warning, Message: warning})
	}
	copy := t
	if copy.ID == "" {
		copy.ID = "draft"
	}
	cfg := model.Config{Tunnels: []model.Tunnel{copy}}
	if copy.Profile != "" {
		if err := a.call(ctx, "profile.list", api.Empty{}, &cfg.Profiles); err != nil {
			return result, err
		}
	}
	if len(copy.Groups) > 0 {
		if err := a.call(ctx, "group.list", api.Empty{}, &cfg.Groups); err != nil {
			return result, err
		}
	}
	structural := cfg.Validate()
	if strings.TrimSpace(t.Name) == "" {
		structural = errors.New("tunnel name must not be empty")
	}
	if structural != nil && result.BuildError == "" {
		result.BuildError = structural.Error()
	}
	if result.Findings == nil {
		result.Findings = []checks.Finding{}
	}
	return result, nil
}

func definitionError(result api.DraftResult) error {
	if result.BuildError != "" {
		return usageError("cannot build tunnel: %s", result.BuildError)
	}
	for _, finding := range result.Findings {
		if finding.Severity == checks.Error {
			return errors.New("tunnel checks failed; fix the errors before saving")
		}
	}
	return nil
}

func (a *app) finishDefinition(ctx context.Context, t model.Tunnel, options defineOptions, warnings []string) error {
	result, err := a.evaluateDefinition(ctx, t, warnings)
	if err != nil {
		return err
	}
	return a.saveDefinition(ctx, t, options, result)
}

func (a *app) saveDefinition(ctx context.Context, t model.Tunnel, options defineOptions, result api.DraftResult) error {
	validationErr := definitionError(result)
	id := t.ID
	if validationErr == nil && !options.dryRun {
		if err := a.call(ctx, "tunnel.save", api.SaveTunnelParams{Tunnel: t, Start: options.start}, &id); err != nil {
			return err
		}
	}
	if a.json {
		if err := a.writeJSON(map[string]any{
			"id": id, "preview": result.Preview, "buildError": result.BuildError,
			"findings": result.Findings, "dryRun": options.dryRun,
			"saved":   validationErr == nil && !options.dryRun,
			"started": validationErr == nil && !options.dryRun && options.start,
		}); err != nil {
			return err
		}
	} else {
		if options.dryRun && result.Preview != "" {
			if _, err := fmt.Fprintln(a.io.Out, result.Preview); err != nil {
				return err
			}
		}
		if err := a.writeFindings(result.Findings); err != nil {
			return err
		}
		if validationErr == nil && !options.dryRun {
			if _, err := fmt.Fprintln(a.io.Out, id); err != nil {
				return err
			}
		}
	}
	return validationErr
}

func importCommand(a *app) *cobra.Command {
	var name string
	var options defineOptions
	cmd := &cobra.Command{
		Use:     "import [--name N] -- <cmd> [args...]",
		Short:   "Save a traditional command as a tunnel",
		Long:    "Import a traditional ssh -N -L ..., ngrok http ..., cloudflared tunnel ... or shell command, then evaluate and save it. Arguments after -- retain their quoting; a single argument can contain an entire pasted command. Importer warnings are shown. Inline credentials are rejected: store them with dtt secret set and use add --secret NAME instead.",
		Example: "  dtt import --name db -- ssh -N -L 15432:127.0.0.1:5432 server1\n  dtt import --dry-run -- 'ngrok http 3000'",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			line := args[0]
			if len(args) > 1 {
				line = defineJoinArgs(args)
			}
			var imported importer.Result
			if err := a.call(cmd.Context(), "tunnel.import", api.ImportParams{Line: line}, &imported); err != nil {
				return err
			}
			if len(imported.Secrets) > 0 {
				return usageError("import contains inline credentials; store them with dtt secret set, then use add --secret NAME (a keychain reference, never a value)")
			}
			var defaults model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.newDraft", api.NewDraftParams{Kind: imported.Tunnel.Type}, &defaults); err != nil {
				return err
			}
			t := imported.Tunnel
			t.ID, t.Reconnect = "", defaults.Reconnect
			t.Name = name
			if t.Name == "" {
				t.Name = string(t.Type) + " tunnel"
			}
			if err := options.apply(cmd.Context(), a, &t); err != nil {
				return err
			}
			return a.finishDefinition(cmd.Context(), t, options, imported.Warnings)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name for the imported tunnel (default: TYPE tunnel)")
	options.flags(cmd)
	return cmd
}

// Preview supplies the same argument quoting used by the engine. In particular,
// literal shell metacharacters in an argv element must not become shell syntax.
func defineJoinArgs(args []string) string {
	return (command.Spec{Tool: args[0], Args: args[1:]}).Preview(platform.Dialect())
}

var defineSetKeys = []string{"favorite", "name", "reconnect", "start_on_launch"}

func setCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "set <t> key=value...",
		Short:   "Change tunnel fields without an editor",
		Long:    "Change explicit tunnel fields and evaluate before saving. Supported keys are favorite, name, reconnect and start_on_launch. Renaming never changes the tunnel ID. Use edit for backend-specific settings; replaces hand-editing tunnels.toml for simple changes.",
		Example: "  dtt set db favorite=true reconnect=false\n  dtt set db 'name=Production database' start_on_launch=true",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse before connecting so unknown keys always produce a usage error.
			changes := make(map[string]string, len(args)-1)
			for _, arg := range args[1:] {
				key, value, ok := strings.Cut(arg, "=")
				if !ok || !defineKnownSetKey(key) {
					return usageError("invalid setting %q; valid keys: %s", key, strings.Join(defineSetKeys, ", "))
				}
				if key != "name" {
					if _, err := strconv.ParseBool(value); err != nil {
						return usageError("%s must be a boolean", key)
					}
				} else if strings.TrimSpace(value) == "" {
					return usageError("name must not be empty")
				}
				changes[key] = value
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			t := view.Tunnel
			for key, value := range changes {
				flag, _ := strconv.ParseBool(value)
				switch key {
				case "name":
					t.Name = value
				case "favorite":
					t.Favorite = flag
				case "reconnect":
					t.Reconnect = flag
				case "start_on_launch":
					t.StartOnLaunch = flag
				}
			}
			return a.finishDefinition(cmd.Context(), t, defineOptions{}, nil)
		},
	}
}

func defineKnownSetKey(key string) bool {
	for _, valid := range defineSetKeys {
		if key == valid {
			return true
		}
	}
	return false
}

func dupCommand(a *app) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "dup <t> --name N",
		Short:   "Save a copy of a tunnel",
		Long:    "Copy a tunnel through the daemon editor's duplicate draft operation, evaluate it and save it with a new stable ID. The original and its profile memberships are preserved. Replaces copying a tunnel block in tunnels.toml.",
		Example: "  dtt dup db --name db-staging",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return usageError("--name is required and must not be empty")
			}
			view, err := a.resolveTunnel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var t model.Tunnel
			if err := a.call(cmd.Context(), "tunnel.duplicateDraft", api.IDParams{ID: view.Tunnel.ID}, &t); err != nil {
				return err
			}
			// duplicateDraft suggests an ID but save only accepts empty IDs for
			// new tunnels. Let save allocate once, using the requested name.
			t.ID, t.Name = "", name
			return a.finishDefinition(cmd.Context(), t, defineOptions{}, nil)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name of the new tunnel (required)")
	return cmd
}

func rmCommand(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <t>...",
		Short:   "Delete saved tunnel definitions",
		Long:    "Delete saved tunnel definitions and their profile references. Ask y/N on a TTY; --yes is required with redirected input or --no-input. Resolve every reference before deleting anything. Replaces removing tunnel blocks from tunnels.toml.",
		Example: "  dtt rm db\n  dtt rm db staging --yes --json",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes && !a.canPrompt() {
				return usageError("rm requires --yes without a TTY or with --no-input")
			}
			ids := []string{}
			seen := map[string]bool{}
			for _, ref := range args {
				view, err := a.resolveTunnel(cmd.Context(), ref)
				if err != nil {
					return err
				}
				if !seen[view.Tunnel.ID] {
					ids = append(ids, view.Tunnel.ID)
					seen[view.Tunnel.ID] = true
				}
			}
			if !yes {
				confirmed, err := a.defineConfirm("Delete " + strings.Join(ids, ", ") + "? [y/N] ")
				if err != nil {
					return err
				}
				if !confirmed {
					if a.json {
						return a.writeJSON(map[string]any{"ids": []string{}, "deleted": false})
					}
					return nil
				}
			}
			for _, id := range ids {
				if err := a.call(cmd.Context(), "tunnel.delete", api.IDParams{ID: id}, &api.Empty{}); err != nil {
					return &tunnelError{err: err, tunnel: id}
				}
			}
			if a.json {
				return a.writeJSON(map[string]any{"ids": ids, "deleted": true})
			}
			_, err := fmt.Fprintln(a.io.Out, strings.Join(ids, "\n"))
			return err
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm deletion without prompting (required without a TTY)")
	return cmd
}

func (a *app) defineConfirm(prompt string) (bool, error) {
	if _, err := fmt.Fprint(a.io.Err, prompt); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(a.io.In)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	answer := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
