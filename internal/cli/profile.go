package cli

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(profileCommand) }

func profileCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "profile", Short: "Organize tunnels into profiles",
		Long:    "Manage named tunnel groups, their SSH configuration and activation behavior. Profile references accept an exact ID, exact name or unique ID prefix.",
		Example: "  dtt profile ls\n  dtt profile use work", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(profileListCommand(a), profileShowCommand(a), profileAddCommand(a), profileUseCommand(a), profileAutostartCommand(a), profileRemoveCommand(a))
	for _, action := range []string{"start", "stop"} {
		label := "Start"
		if action == "stop" {
			label = "Stop"
		}
		cmd.AddCommand(&cobra.Command{
			Use: action + " <p>", Short: label + " all members of a profile",
			Long:    label + " every tunnel belonging to a profile, without changing the active profile or its autostart settings.",
			Example: "  dtt profile " + action + " work\n  dtt profile " + action + " work --json", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				p, err := a.resolveProfile(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if err := a.call(cmd.Context(), "profile."+action, api.IDParams{ID: p.ID}, nil); err != nil {
					return err
				}
				return a.profileActionResult(p.ID, action+" complete")
			},
		})
	}
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
	for _, p := range profiles {
		if p.ID == ref {
			return p, nil
		}
	}
	var names, prefixes []model.Profile
	for _, p := range profiles {
		if p.Name == ref {
			names = append(names, p)
		}
		if ref != "" && strings.HasPrefix(p.ID, ref) {
			prefixes = append(prefixes, p)
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
		return model.Profile{}, usageError("unknown profile %q", ref)
	}
	ids := make([]string, len(matches))
	for i, p := range matches {
		ids[i] = p.ID
	}
	sort.Strings(ids)
	return model.Profile{}, usageError("ambiguous profile %q; matches: %s", ref, strings.Join(ids, ", "))
}

type profileMember struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Autostart bool   `json:"autostart"`
}

type profileView struct {
	model.Profile
	Active  bool            `json:"active"`
	Members []profileMember `json:"members"`
}

func profileMakeView(p model.Profile, overview api.Overview) profileView {
	view := profileView{Profile: p, Active: p.ID == overview.ActiveProfile, Members: []profileMember{}}
	for _, tunnel := range overview.Tunnels {
		if slices.Contains(tunnel.Tunnel.Profiles, p.ID) {
			view.Members = append(view.Members, profileMember{ID: tunnel.Tunnel.ID, Name: tunnel.Tunnel.Name, Autostart: slices.Contains(p.Autostart, tunnel.Tunnel.ID)})
		}
	}
	return view
}

func profileListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "ls", Short: "List profiles and their members",
		Long:    "List profiles with an active marker, member IDs and autostart member IDs. Deleting a profile never deletes its tunnels.",
		Example: "  dtt profile ls\n  dtt profile ls --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var overview api.Overview
			if err := a.call(cmd.Context(), "config.overview", api.Empty{}, &overview); err != nil {
				return err
			}
			views := make([]profileView, 0, len(overview.Profiles))
			rows := make([][]string, 0, len(overview.Profiles))
			for _, p := range overview.Profiles {
				view := profileMakeView(p, overview)
				views = append(views, view)
				marker := ""
				if view.Active {
					marker = "*"
				}
				members := make([]string, 0, len(view.Members))
				for _, member := range view.Members {
					members = append(members, member.ID)
				}
				rows = append(rows, []string{marker, p.ID, p.Name, strings.Join(members, ", "), strings.Join(p.Autostart, ", ")})
			}
			if a.json {
				return a.writeJSON(map[string]any{"profiles": views, "activeProfile": overview.ActiveProfile})
			}
			return a.table([]string{"ACTIVE", "ID", "NAME", "MEMBERS", "AUTOSTART"}, rows)
		},
	}
}

func profileShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "show <p>", Short: "Inspect a profile and its members",
		Long:    "Show a profile's active state, SSH config, activation behavior, members and each member's autostart setting.",
		Example: "  dtt profile show work\n  dtt profile show 'Work projects' --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var overview api.Overview
			if err := a.call(cmd.Context(), "config.overview", api.Empty{}, &overview); err != nil {
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
			behavior := p.OnActivate
			if behavior == "" {
				behavior = "filter"
			}
			if _, err := fmt.Fprintf(a.io.Out, "%s (%s)\nActive: %t\nSSH config: %s\nOn activate: %s\nStop non-members: %t\n", p.Name, p.ID, view.Active, p.SSHConfig, behavior, p.StopNonMembers); err != nil {
				return err
			}
			rows := make([][]string, 0, len(view.Members))
			for _, member := range view.Members {
				rows = append(rows, []string{member.ID, member.Name, strconv.FormatBool(member.Autostart)})
			}
			return a.table([]string{"ID", "NAME", "AUTOSTART"}, rows)
		},
	}
}

func profileAddCommand(a *app) *cobra.Command {
	p := model.Profile{OnActivate: "filter"}
	cmd := &cobra.Command{
		Use: "add <name>", Short: "Create a profile",
		Long:    "Create an empty profile with optional SSH config and activation behavior. Assign tunnels with dtt add --profile or dtt set. By default activating the profile only changes the filter.",
		Example: "  dtt profile add Work --ssh-config ~/.ssh/work\n  dtt profile add Work --on-activate start_autostart --stop-non-members", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p.Name = strings.TrimSpace(args[0])
			if p.Name == "" {
				return usageError("profile name must not be empty")
			}
			if p.OnActivate != "filter" && p.OnActivate != "start_autostart" {
				return usageError("--on-activate must be filter or start_autostart")
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
	cmd.Flags().StringVar(&p.OnActivate, "on-activate", "filter", "Activation behavior: filter or start_autostart")
	cmd.Flags().BoolVar(&p.StopNonMembers, "stop-non-members", false, "Stop running non-members on activation")
	return cmd
}

func profileUseCommand(a *app) *cobra.Command {
	var none bool
	cmd := &cobra.Command{
		Use: "use <p> | --none", Short: "Set or clear the active profile",
		Long:    "Activate a profile, applying its start-autostart and stop-non-members behavior. --none clears the active profile without starting or stopping tunnels.",
		Example: "  dtt profile use work\n  dtt profile use --none", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if none && len(args) != 0 || !none && len(args) != 1 {
				return usageError("specify a profile or --none, but not both")
			}
			id := ""
			if !none {
				p, err := a.resolveProfile(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				id = p.ID
			}
			if err := a.call(cmd.Context(), "profile.activate", api.IDParams{ID: id}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"activeProfile": id})
			}
			if id == "" {
				a.diagnostic("Active profile cleared.")
			} else {
				a.diagnostic("Active profile: %s", id)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&none, "none", false, "Clear the active profile")
	return cmd
}

func profileAutostartCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "autostart <p> <t> on|off", Short: "Set a profile member's autostart behavior",
		Long:    "Enable or disable autostart for an existing member. Autostart members run when a profile with start_autostart activation behavior is activated.",
		Example: "  dtt profile autostart work db on\n  dtt profile autostart work db off --json", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[2] != "on" && args[2] != "off" {
				return usageError("autostart must be on or off")
			}
			p, err := a.resolveProfile(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			tunnel, err := a.resolveTunnel(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			if !slices.Contains(tunnel.Tunnel.Profiles, p.ID) {
				return usageError("tunnel %q is not a member of profile %q", tunnel.Tunnel.ID, p.ID)
			}
			on := args[2] == "on"
			if err := a.call(cmd.Context(), "profile.setAutostart", api.AutostartParams{ID: p.ID, TunnelID: tunnel.Tunnel.ID, On: on}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"id": p.ID, "tunnelId": tunnel.Tunnel.ID, "autostart": on})
			}
			a.diagnostic("Profile %s: %s autostart %s.", p.ID, tunnel.Tunnel.ID, args[2])
			return nil
		},
	}
}

func profileRemoveCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "rm <p>", Short: "Delete a profile, keeping its tunnels",
		Long:    "Delete a profile and remove its membership from tunnels. Tunnel definitions and running processes are kept. Removing the active profile clears the active selection.",
		Example: "  dtt profile rm work\n  dtt profile rm work --json", Args: cobra.ExactArgs(1),
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
