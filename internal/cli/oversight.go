package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
)

func init() {
	register(leasesCommand)
	register(approvalsCommand)
	register(func(a *app) *cobra.Command { return decideApprovalCommand(a, true) })
	register(func(a *app) *cobra.Command { return decideApprovalCommand(a, false) })
	register(auditCommand)
}

func leasesCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "leases", Short: "List clients holding tunnels and their expiries",
		Long:    "Inspect the claims keeping tunnels running, rather than guessing from process IDs. Shows lease ID, tunnel, holder, effective expiry, whether it started the tunnel and whether it is detached. Detached person leases survive CLI exit; an expiry of never means explicit stop or revoke is required.",
		Example: "  dtt leases\n  dtt leases --json\n  dtt leases revoke ls_123",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			leases := []api.Lease{}
			if err := a.call(cmd.Context(), "lease.list", api.Empty{}, &leases); err != nil {
				return err
			}
			if leases == nil {
				leases = []api.Lease{}
			}
			if a.json {
				return a.writeJSON(map[string]any{"leases": leases})
			}
			rows := make([][]string, 0, len(leases))
			for _, lease := range leases {
				expires := lease.ExpiresAt
				if lease.GraceUntil != nil && (expires.IsZero() || lease.GraceUntil.Before(expires)) {
					expires = *lease.GraceUntil
				}
				expiry := "never"
				if !expires.IsZero() {
					expiry = expires.Format(time.RFC3339)
				}
				if lease.Revoked {
					expiry = "revoked"
				}
				rows = append(rows, []string{lease.ID, lease.TunnelID, lease.Holder.Kind + "/" + lease.Holder.Name, expiry, strconv.FormatBool(lease.StartedIt), strconv.FormatBool(lease.Detached)})
			}
			return a.table([]string{"ID", "TUNNEL", "HOLDER", "EXPIRES", "STARTED-IT", "DETACHED"}, rows)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use: "revoke <id>", Short: "Revoke a lease and stop its unowned tunnel",
		Long:    "Revoke a client's claim immediately. If no other lease or person pin keeps the tunnel running, its process tree is stopped. Replaces killing an agent's tunnel process directly; the agent receives lease_revoked on its next operation.",
		Example: "  dtt leases revoke ls_123\n  dtt leases revoke ls_123 --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.call(cmd.Context(), "lease.revoke", api.IDParams{ID: args[0]}, nil); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"id": args[0], "revoked": true})
			}
			return nil
		},
	})
	return cmd
}

func approvalsCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "approvals", Short: "List pending agent requests for a person",
		Long:    "Show pending fingerprint-bound agent requests with their summary, exact argv, actor, purpose, allowed scopes, expiry and confirmation code. Codes are intentionally visible to person clients, never to agents. Approval does not run anything: the requesting agent must retry. Use approvals approve/deny, or their top-level approve/deny aliases.",
		Example: "  dtt approvals\n  dtt approvals --json\n  dtt approvals approve ap_123 --scope once",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var all []api.Approval
			if err := a.call(cmd.Context(), "approval.list", api.Empty{}, &all); err != nil {
				return err
			}
			pending := []api.Approval{}
			for _, request := range all {
				if request.Status == "pending" {
					pending = append(pending, request)
				}
			}
			if a.json {
				return a.writeJSON(map[string]any{"approvals": pending})
			}
			for _, request := range pending {
				if err := writeApproval(a.io.Out, request, true); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.AddCommand(decideApprovalCommand(a, true), decideApprovalCommand(a, false))
	return cmd
}

func writeApproval(out io.Writer, request api.Approval, showCode bool) error {
	argv := ""
	if len(request.Argv) > 0 {
		argv = defineJoinArgs(request.Argv)
	}
	_, err := fmt.Fprintf(out, "%s: %s\n  argv: %s\n  actor: %s/%s\n  purpose: %s\n  scopes: %s\n  expires: %s\n", request.ID, request.Summary, argv, request.Actor.Kind, request.Actor.Name, request.Purpose, strings.Join(request.Choices, ", "), request.ExpiresAt.Format(time.RFC3339))
	if err == nil && showCode {
		_, err = fmt.Fprintln(out, "  code: "+request.Code)
	}
	return err
}

func decideApprovalCommand(a *app, approve bool) *cobra.Command {
	verb := "deny"
	if approve {
		verb = "approve"
	}
	var scope string
	cmd := &cobra.Command{
		Use: verb + " <id>", Short: strings.ToUpper(verb[:1]) + verb[1:] + " a pending agent request",
		Long:    "Deny an agent request without granting permission. No terminal or confirmation code input is needed to deny. The request is fingerprint-bound, and deciding it never starts a tunnel. This command is also available as dtt deny.",
		Example: "  dtt approvals deny ap_123\n  dtt deny ap_123 --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if approve && !a.canPrompt() {
				return api.NewError(api.NeedsUser, "approval requires a TTY and the code shown by dtt approvals; --no-input cannot approve")
			}
			if approve && scope != "once" && scope != "session" && scope != "always" {
				return usageError("--scope must be once, session or always")
			}
			var request api.Approval
			if err := a.call(cmd.Context(), "approval.get", api.IDParams{ID: args[0]}, &request); err != nil {
				return err
			}
			code := request.Code
			chosen := scope
			if approve {
				if !slices.Contains(request.Choices, scope) {
					return usageError("scope %q is not allowed; choose %s", scope, strings.Join(request.Choices, ", "))
				}
				if request.Status != "pending" {
					return api.NewError(api.PolicyDenied, "approval is no longer pending")
				}
				// Show the request on stderr so --json stdout remains a single object;
				// deliberately do not supply its code to the confirmation prompt.
				if err := writeApproval(a.io.Err, request, false); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(a.io.Err, "Selected scope: %s\nEnter the code shown by dtt approvals: ", scope); err != nil {
					return err
				}
				var err error
				code, err = a.readPromptInput(cmd.Context(), true)
				fmt.Fprintln(a.io.Err)
				if err != nil {
					return err
				}
				code = strings.TrimSpace(code)
			} else if !slices.Contains(request.Choices, chosen) && len(request.Choices) > 0 {
				chosen = request.Choices[0]
			}
			var decided api.Approval
			if err := a.call(cmd.Context(), "approval.decide", api.DecideApprovalParams{ID: args[0], Decision: verb, Scope: chosen, Code: code}, &decided); err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"approval": decided})
			}
			_, err := fmt.Fprintf(a.io.Out, "%s %s\n", decided.ID, decided.Status)
			return err
		},
	}
	if approve {
		cmd.Long = "Approve a pending fingerprint-bound agent request. Requires a TTY, even with --json, and asks you to type the code displayed in dtt approvals. Codes are read from the terminal, never flags or piped stdin; --no-input exits 4. --scope must be one of the request's allowed scopes. The agent must retry after approval; no command is run by this decision. Also available as dtt approve."
		cmd.Example = "  dtt approvals approve ap_123\n  dtt approve ap_123 --scope session"
		cmd.Flags().StringVar(&scope, "scope", "once", "Permission scope: once, session or always (must be allowed by the request)")
	} else {
		scope = "once"
	}
	return cmd
}

func auditCommand(a *app) *cobra.Command {
	var actor, tunnel, since string
	var follow bool
	cmd := &cobra.Command{
		Use: "audit", Short: "Read or follow the daemon's audit history",
		Long:    "Read retained change history, like tail on an audit log, filtered by exact actor name, exact tunnel ID (including deleted IDs) and an inclusive --since duration ago or RFC3339 timestamp. Filters combine with AND. -f polls audit.read with its persistent cursor until Ctrl-C; --json emits one versioned object per batch in follow mode. Warns if retention overtakes the cursor.",
		Example: "  dtt audit --actor claude-code --since 1h\n  dtt audit --tunnel staging-db --json\n  dtt audit -f --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := api.AuditReadParams{Actor: actor, Tunnel: tunnel, Limit: 200}
			if cmd.Flags().Changed("since") {
				if d, err := time.ParseDuration(since); err == nil && d >= 0 {
					params.Since = time.Now().Add(-d)
				} else {
					value, err := time.Parse(time.RFC3339, since)
					if err != nil {
						return usageError("--since must be a nonnegative duration or RFC3339 timestamp")
					}
					params.Since = value
				}
			}
			return a.readAudit(cmd.Context(), params, follow)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "Exact actor name")
	cmd.Flags().StringVar(&tunnel, "tunnel", "", "Exact tunnel ID (also accepts deleted IDs)")
	cmd.Flags().StringVar(&since, "since", "", "History since a duration ago (e.g. 1h) or RFC3339 timestamp")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow new audit events until interrupted")
	return cmd
}

func (a *app) readAudit(ctx context.Context, params api.AuditReadParams, follow bool) error {
	combined := api.AuditReadResult{Events: []audit.Event{}}
	initial := true
	for {
		var batch api.AuditReadResult
		if err := a.call(ctx, "audit.read", params, &batch); err != nil {
			if follow && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if batch.Events == nil {
			batch.Events = []audit.Event{}
		}
		if batch.Lost {
			a.diagnostic("Warning: audit retention discarded events before cursor %d (oldest retained: %d)", params.After, batch.Oldest)
		}
		if follow {
			if initial || len(batch.Events) > 0 || batch.Lost {
				if err := a.writeAudit(batch); err != nil {
					return err
				}
			}
		} else {
			combined.Events = append(combined.Events, batch.Events...)
			if combined.Oldest == 0 {
				combined.Oldest = batch.Oldest
			}
			combined.Next, combined.Lost = batch.Next, combined.Lost || batch.Lost
		}
		initial = false
		more := batch.Next > params.After && len(batch.Events) > 0
		params.After = batch.Next
		if more {
			continue
		} // Drain bounded API pages before polling or returning.
		if !follow {
			return a.writeAudit(combined)
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (a *app) writeAudit(batch api.AuditReadResult) error {
	if a.json {
		return a.writeJSON(map[string]any{"events": batch.Events, "next": batch.Next, "oldest": batch.Oldest, "lost": batch.Lost})
	}
	rows := make([][]string, 0, len(batch.Events))
	for _, event := range batch.Events {
		rows = append(rows, []string{strconv.FormatUint(event.Sequence, 10), event.Time.Format(time.RFC3339), string(event.Actor.Kind) + "/" + event.Actor.Name, string(event.Action), event.TunnelID, string(event.Result)})
	}
	return a.table([]string{"SEQ", "TIME", "ACTOR", "ACTION", "TUNNEL", "RESULT"}, rows)
}
