package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(profileCommand) }

func profileCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "profile", Short: "Manage configuration shared by tunnels",
		Long:    "Manage named configurations, such as the SSH config file, used by the tunnels that belong to them. A tunnel belongs to at most one profile. To start or stop tunnels together, use devt-tunnel group. Profile references accept an exact ID, exact name or unique ID prefix.",
		Example: "  devt-tunnel profile ls\n  devt-tunnel profile add Work --ssh-config ~/.ssh/work", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(profileListCommand(a), profileShowCommand(a), profileAddCommand(a), profileRemoveCommand(a))
	return cmd
}

func (a *app) resolveProfile(ctx context.Context, ref string) (model.Profile, error) {
	var profiles []model.Profile
	if err := a.call(ctx, "profile.list", api.Empty{}, &profiles); err != nil {
		return model.Profile{}, err
	}
	return profileMatch(profiles, ref)
}

func profileMatch(profiles []model.Profile, ref string) (model.Profile, error) {
	return matchReference("profile", profiles, func(p model.Profile) (string, string) { return p.ID, p.Name }, ref)
}

// matchReference resolves an exact ID, then an exact name, then a unique ID prefix.
func matchReference[T any](kind string, items []T, identify func(T) (string, string), ref string) (T, error) {
	var zero T
	for _, item := range items {
		if id, _ := identify(item); id == ref {
			return item, nil
		}
	}
	var names, prefixes []T
	for _, item := range items {
		id, name := identify(item)
		if name == ref {
			names = append(names, item)
		}
		if ref != "" && strings.HasPrefix(id, ref) {
			prefixes = append(prefixes, item)
		}
	}
	matches := names
	if len(matches) == 0 {
		matches = prefixes
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return zero, usageError("unknown %s %q", kind, ref)
	}
	ids := make([]string, len(matches))
	for i, item := range matches {
		ids[i], _ = identify(item)
	}
	sort.Strings(ids)
	return zero, usageError("ambiguous %s %q; matches: %s", kind, ref, strings.Join(ids, ", "))
}

type profileMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type profileView struct {
	model.Profile
	Members []profileMember `json:"members"`
}

func profileMakeView(p model.Profile, overview api.Overview) profileView {
	view := profileView{Profile: p, Members: []profileMember{}}
	for _, tunnel := range overview.Tunnels {
		if tunnel.Tunnel.Profile == p.ID {
			view.Members = append(view.Members, profileMember{ID: tunnel.Tunnel.ID, Name: tunnel.Tunnel.Name})
		}
	}
	return view
}

func profileListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "ls", Short: "List profiles and their members",
		Long:    "List profiles with their SSH config and member IDs. Deleting a profile never deletes its tunnels.",
		Example: "  devt-tunnel profile ls\n  devt-tunnel profile ls --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			overview, err := a.overview(cmd.Context())
			if err != nil {
				return err
			}
			views := make([]profileView, 0, len(overview.Profiles))
			rows := make([][]string, 0, len(overview.Profiles))
			for _, p := range overview.Profiles {
				view := profileMakeView(p, overview)
				views = append(views, view)
				members := make([]string, 0, len(view.Members))
				for _, member := range view.Members {
					members = append(members, member.ID)
				}
				rows = append(rows, []string{p.ID, p.Name, p.SSHConfig, strings.Join(members, ", ")})
			}
			if a.json {
				return a.writeJSON(map[string]any{"profiles": views})
			}
			return a.table([]string{"ID", "NAME", "SSH CONFIG", "MEMBERS"}, rows)
		},
	}
}

func profileShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "show <p>", Short: "Inspect a profile and its members",
		Long:    "Show a profile's description, SSH config and members.",
		Example: "  devt-tunnel profile show work\n  devt-tunnel profile show 'Work projects' --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overview, err := a.overview(cmd.Context())
			if err != nil {
				return err
			}
			p, err := profileMatch(overview.Profiles, args[0])
			if err != nil {
				return err
			}
			view := profileMakeView(p, overview)
			if a.json {
				return a.writeJSON(map[string]any{"profile": view})
			}
			if _, err := fmt.Fprintf(a.io.Out, "%s (%s)\nDescription: %s\nSSH config: %s\n", p.Name, p.ID, p.Description, p.SSHConfig); err != nil {
				return err
			}
			rows := make([][]string, 0, len(view.Members))
			for _, member := range view.Members {
				rows = append(rows, []string{member.ID, member.Name})
			}
			return a.table([]string{"ID", "NAME"}, rows)
		},
	}
}

func profileAddCommand(a *app) *cobra.Command {
	var p model.Profile
	cmd := &cobra.Command{
		Use: "add <name>", Short: "Create a profile",
		Long:    "Create an empty profile with an optional SSH config. Put tunnels in it with devt-tunnel add --profile or devt-tunnel save --profile.",
		Example: "  devt-tunnel profile add Work --ssh-config ~/.ssh/work", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p.Name = strings.TrimSpace(args[0])
			if p.Name == "" {
				return usageError("profile name must not be empty")
			}
			var id string
			if err := a.call(cmd.Context(), "profile.save", api.SaveProfileParams{Profile: p, Members: []string{}}, &id); err != nil {
				return err
			}
			p.ID = id
			if a.json {
				return a.writeJSON(map[string]any{"profile": p})
			}
			_, err := fmt.Fprintln(a.io.Out, id)
			return err
		},
	}
	cmd.Flags().StringVar(&p.SSHConfig, "ssh-config", "", "SSH configuration file used by members")
	cmd.Flags().StringVar(&p.Description, "description", "", "What the profile is for")
	return cmd
}

func profileRemoveCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "rm <p>", Short: "Delete a profile, keeping its tunnels",
		Long:    "Delete a profile. Its tunnels are kept, left running, and go back to the default SSH config.",
		Example: "  devt-tunnel profile rm work\n  devt-tunnel profile rm work --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.resolveProfile(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := a.call(cmd.Context(), "profile.delete", api.IDParams{ID: p.ID}, nil); err != nil {
				return err
			}
			return a.profileActionResult(p.ID, "deleted; tunnels kept")
		},
	}
}

func (a *app) profileActionResult(id, status string) error {
	if a.json {
		return a.writeJSON(map[string]any{"id": id, "status": status})
	}
	a.diagnostic("Profile %s: %s.", id, status)
	return nil
}
