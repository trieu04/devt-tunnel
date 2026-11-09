package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func init() { register(groupCommand) }

func groupCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "group", Short: "Operate on sets of tunnels together",
		Long:    "Manage named sets of tunnels and start or stop them as one task. A tunnel can be in many groups, independently of its profile. Group references accept an exact ID, exact name or unique ID prefix.",
		Example: "  devt-tunnel group add Backend\n  devt-tunnel group assign backend db api\n  devt-tunnel group start backend", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(groupListCommand(a), groupShowCommand(a), groupAddCommand(a), groupMembershipCommand(a, "assign"), groupMembershipCommand(a, "unassign"), groupRemoveCommand(a))
	for _, action := range []string{"start", "stop"} {
		label := "Start"
		if action == "stop" {
			label = "Stop"
		}
		cmd.AddCommand(&cobra.Command{
			Use: action + " <g>", Short: label + " all members of a group",
			Long:    label + " every tunnel belonging to a group.",
			Example: "  devt-tunnel group " + action + " backend\n  devt-tunnel group " + action + " backend --json", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				g, err := a.resolveGroup(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if err := a.call(cmd.Context(), "group."+action, api.IDParams{ID: g.ID}, nil); err != nil {
					return err
				}
				return a.groupActionResult(g.ID, action+" complete")
			},
		})
	}
	return cmd
}

func (a *app) resolveGroup(ctx context.Context, ref string) (model.Group, error) {
	var groups []model.Group
	if err := a.call(ctx, "group.list", api.Empty{}, &groups); err != nil {
		return model.Group{}, err
	}
	return matchReference("group", groups, func(g model.Group) (string, string) { return g.ID, g.Name }, ref)
}

type groupMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type groupView struct {
	model.Group
	Members []groupMember `json:"members"`
}

func groupMakeView(g model.Group, overview api.Overview) groupView {
	view := groupView{Group: g, Members: []groupMember{}}
	for _, tunnel := range overview.Tunnels {
		if slices.Contains(tunnel.Tunnel.Groups, g.ID) {
			view.Members = append(view.Members, groupMember{ID: tunnel.Tunnel.ID, Name: tunnel.Tunnel.Name})
		}
	}
	return view
}

func memberIDs(members []groupMember) []string {
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}
	return ids
}

func (a *app) overview(ctx context.Context) (api.Overview, error) {
	var overview api.Overview
	err := a.call(ctx, "config.overview", api.Empty{}, &overview)
	return overview, err
}

func groupListCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "ls", Short: "List groups and their members",
		Long:    "List groups with their member IDs. Deleting a group never deletes its tunnels.",
		Example: "  devt-tunnel group ls\n  devt-tunnel group ls --json", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			overview, err := a.overview(cmd.Context())
			if err != nil {
				return err
			}
			views := make([]groupView, 0, len(overview.Groups))
			rows := make([][]string, 0, len(overview.Groups))
			for _, g := range overview.Groups {
				view := groupMakeView(g, overview)
				views = append(views, view)
				rows = append(rows, []string{g.ID, g.Name, strings.Join(memberIDs(view.Members), ", ")})
			}
			if a.json {
				return a.writeJSON(map[string]any{"groups": views})
			}
			return a.table([]string{"ID", "NAME", "MEMBERS"}, rows)
		},
	}
}

func groupShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "show <g>", Short: "Inspect a group and its members",
		Long:    "Show a group's description and members.",
		Example: "  devt-tunnel group show backend\n  devt-tunnel group show Backend --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overview, err := a.overview(cmd.Context())
			if err != nil {
				return err
			}
			g, err := matchReference("group", overview.Groups, func(g model.Group) (string, string) { return g.ID, g.Name }, args[0])
			if err != nil {
				return err
			}
			view := groupMakeView(g, overview)
			if a.json {
				return a.writeJSON(map[string]any{"group": view})
			}
			if _, err := fmt.Fprintf(a.io.Out, "%s (%s)\nDescription: %s\n", g.Name, g.ID, g.Description); err != nil {
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

func groupAddCommand(a *app) *cobra.Command {
	var g model.Group
	cmd := &cobra.Command{
		Use: "add <name>", Short: "Create a group",
		Long:    "Create an empty group. Assign tunnels with devt-tunnel group assign or devt-tunnel add --group.",
		Example: "  devt-tunnel group add Backend\n  devt-tunnel group add Staging --description 'Everything for staging'", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g.Name = strings.TrimSpace(args[0])
			if g.Name == "" {
				return usageError("group name must not be empty")
			}
			var id string
			if err := a.call(cmd.Context(), "group.save", api.SaveGroupParams{Group: g, Members: []string{}}, &id); err != nil {
				return err
			}
			g.ID = id
			if a.json {
				return a.writeJSON(map[string]any{"group": g})
			}
			_, err := fmt.Fprintln(a.io.Out, id)
			return err
		},
	}
	cmd.Flags().StringVar(&g.Description, "description", "", "What the group is for")
	return cmd
}

func groupMembershipCommand(a *app, verb string) *cobra.Command {
	short := "Add tunnels to a group"
	if verb == "unassign" {
		short = "Remove tunnels from a group"
	}
	return &cobra.Command{
		Use: verb + " <g> <t>...", Short: short,
		Long:    short + ". Tunnels keep their other groups and their profile, and running tunnels are left running.",
		Example: "  devt-tunnel group " + verb + " backend db api", Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			overview, err := a.overview(cmd.Context())
			if err != nil {
				return err
			}
			g, err := matchReference("group", overview.Groups, func(g model.Group) (string, string) { return g.ID, g.Name }, args[0])
			if err != nil {
				return err
			}
			members := memberIDs(groupMakeView(g, overview).Members)
			for _, ref := range args[1:] {
				tunnel, err := a.resolveTunnel(cmd.Context(), ref)
				if err != nil {
					return err
				}
				members = slices.DeleteFunc(members, func(id string) bool { return id == tunnel.Tunnel.ID })
				if verb == "assign" {
					members = append(members, tunnel.Tunnel.ID)
				}
			}
			if err := a.call(cmd.Context(), "group.save", api.SaveGroupParams{Group: g, Members: members}, nil); err != nil {
				return err
			}
			return a.groupActionResult(g.ID, verb+" complete")
		},
	}
}

func groupRemoveCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use: "rm <g>", Short: "Delete a group, keeping its tunnels",
		Long:    "Delete a group and remove its membership from tunnels. Tunnel definitions and running processes are kept.",
		Example: "  devt-tunnel group rm backend\n  devt-tunnel group rm backend --json", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := a.resolveGroup(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := a.call(cmd.Context(), "group.delete", api.IDParams{ID: g.ID}, nil); err != nil {
				return err
			}
			return a.groupActionResult(g.ID, "deleted; tunnels kept")
		},
	}
}

func (a *app) groupActionResult(id, status string) error {
	if a.json {
		return a.writeJSON(map[string]any{"id": id, "status": status})
	}
	a.diagnostic("Group %s: %s.", id, status)
	return nil
}
