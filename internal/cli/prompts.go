package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
	"golang.org/x/term"
)

func init() { register(promptsCommand) }

func promptsCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use: "prompts", Short: "List SSH questions waiting for an answer",
		Long:    "List pending SSH questions, such as the host-key and password questions normally displayed by ssh. Answers are never shown. Use dtt prompts answer to respond or cancel a question.",
		Example: "  dtt prompts\n  dtt prompts --json\n  printf 'yes\\n' | dtt prompts answer PROMPT_ID --stdin",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			prompts, err := a.pendingPrompts(cmd.Context())
			if err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"prompts": prompts})
			}
			rows := make([][]string, 0, len(prompts))
			for _, pending := range prompts {
				text := strings.Join(strings.Fields(pending.Prompt.Text), " ")
				rows = append(rows, []string{pending.ID, pending.Prompt.TunnelID, string(pending.Prompt.Kind), text})
			}
			return a.table([]string{"ID", "TUNNEL", "KIND", "QUESTION"}, rows)
		},
	}
	cmd.AddCommand(answerPromptCommand(a))
	return cmd
}

func answerPromptCommand(a *app) *cobra.Command {
	var stdin, cancel bool
	cmd := &cobra.Command{
		Use: "answer <id>", Short: "Answer or cancel a pending SSH question",
		Long:    "Answer an SSH question, like typing into ssh's password prompt. Read the answer from a hidden TTY prompt or --stdin, never from a flag or command argument. Without a TTY, --stdin is required (exit 4 otherwise). --cancel cancels without reading input. Ctrl-C while answering cancels the question.",
		Example: "  dtt prompts answer PROMPT_ID\n  printf 'yes\\n' | dtt prompts answer PROMPT_ID --stdin\n  dtt prompts answer PROMPT_ID --cancel",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if stdin && cancel {
				return usageError("--stdin and --cancel cannot be used together")
			}
			prompts, err := a.pendingPrompts(cmd.Context())
			if err != nil {
				return err
			}
			var pending *api.PendingPrompt
			for i := range prompts {
				if prompts[i].ID == args[0] {
					pending = &prompts[i]
					break
				}
			}
			if pending == nil {
				return usageError("unknown pending prompt %q; run dtt prompts", args[0])
			}
			if cancel {
				err = a.call(cmd.Context(), "prompt.answer", api.AnswerPromptParams{ID: pending.ID, Cancelled: true}, nil)
			} else {
				err = a.answerPendingPrompt(cmd.Context(), *pending, stdin, false)
			}
			if err != nil {
				return err
			}
			if a.json {
				return a.writeJSON(map[string]any{"id": pending.ID, "cancelled": cancel})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read the answer's first line from standard input")
	cmd.Flags().BoolVar(&cancel, "cancel", false, "Cancel the question without reading an answer")
	return cmd
}

func (a *app) pendingPrompts(ctx context.Context) ([]api.PendingPrompt, error) {
	prompts := []api.PendingPrompt{}
	if err := a.call(ctx, "prompt.list", api.Empty{}, &prompts); err != nil {
		return nil, err
	}
	if prompts == nil {
		prompts = []api.PendingPrompt{}
	}
	return prompts, nil
}

// A coalesced wakeup avoids blocking the client's event dispatcher while a user
// is typing. The list is the source of truth, so stale/duplicate events cannot
// make us answer a question twice or answer a question removed by another client.
func (a *app) subscribePromptChanges(ctx context.Context) (<-chan struct{}, func(), error) {
	c, err := a.client(ctx)
	if err != nil {
		return nil, nil, err
	}
	changed := make(chan struct{}, 1)
	c.Events(func(event api.Event) {
		if event.Type == api.EventPromptAdded {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	cleanup := func() { c.Events(nil) }
	if err := a.call(ctx, api.EventsSubscribe, api.Empty{}, nil); err != nil {
		cleanup()
		return nil, nil, err
	}
	return changed, cleanup, nil
}

func (a *app) handleTunnelPrompts(ctx context.Context, ids map[string]bool) error {
	prompts, err := a.pendingPrompts(ctx)
	if err != nil {
		return err
	}
	for _, pending := range prompts {
		if !ids[pending.Prompt.TunnelID] {
			continue
		}
		if err := a.answerPendingPrompt(ctx, pending, false, true); err != nil {
			return err
		}
	}
	return nil
}

func needsPromptInput(pending api.PendingPrompt) error {
	return &tunnelError{
		err:    api.NewError(api.NeedsUser, "SSH question needs an answer; run dtt prompts (or dtt prompts answer "+pending.ID+" --stdin)"),
		tunnel: pending.Prompt.TunnelID, hint: "dtt prompts",
	}
}

func (a *app) answerPendingPrompt(ctx context.Context, pending api.PendingPrompt, stdin, inline bool) error {
	if !stdin && !a.canPrompt() {
		return needsPromptInput(pending)
	}
	if !stdin {
		// Questions are not informational diagnostics: even --quiet must show them.
		if _, err := fmt.Fprint(a.io.Err, pending.Prompt.Text+" "); err != nil {
			return err
		}
	}
	hidden := !stdin && (!inline || pending.Prompt.Kind != askpass.KindHostKey)
	value, err := a.readPromptInput(ctx, hidden)
	if !stdin && hidden {
		fmt.Fprintln(a.io.Err)
	}
	if err == nil {
		err = a.call(ctx, "prompt.answer", api.AnswerPromptParams{ID: pending.ID, Value: value}, nil)
		if err == nil || ctx.Err() == nil {
			return err
		}
	}
	// The signal context is already cancelled on Ctrl-C. Use a bounded,
	// independent context so that cancellation actually reaches the broker,
	// including an interrupt between reading the answer and sending the RPC.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
	defer cancel()
	if cancelErr := a.call(cleanup, "prompt.answer", api.AnswerPromptParams{ID: pending.ID, Cancelled: true}, nil); cancelErr != nil {
		return fmt.Errorf("%w (could not cancel SSH question: %v)", err, cancelErr)
	}
	return err
}

// Reading a terminal is blocking, unlike an RPC. Let the signal context unblock
// the command and restore echo on interruption. A blocked reader is left only
// on an interrupted command; the CLI exits without reading stdin again.
func (a *app) readPromptInput(ctx context.Context, hidden bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var fd int
	if hidden {
		terminal, ok := a.io.In.(interface{ Fd() uintptr })
		if !ok {
			return "", errors.New("input is not a terminal")
		}
		fd = int(terminal.Fd())
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		defer term.Restore(fd, state)
	}
	type input struct {
		value string
		err   error
	}
	done := make(chan input, 1)
	go func() {
		if hidden {
			value, err := term.ReadPassword(fd)
			done <- input{string(value), err}
		} else {
			value, err := readPromptLine(a.io.In)
			done <- input{value, err}
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-done:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return result.value, result.err
	}
}

// Read exactly one line, without consuming the next answer from a TTY. Preserve
// spaces and accept a final line without a newline, as with printf %s VALUE.
func readPromptLine(in io.Reader) (string, error) {
	var text strings.Builder
	var b [1]byte
	for text.Len() < api.MaxMessageSize/2 {
		n, err := in.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				return strings.TrimSuffix(text.String(), "\r"), nil
			}
			text.WriteByte(b[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && text.Len() > 0 {
				return text.String(), nil
			}
			return "", err
		}
	}
	return "", errors.New("SSH answer is too long")
}
