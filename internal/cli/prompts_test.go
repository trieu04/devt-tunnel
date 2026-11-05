package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/askpass"
)

func promptFixture() []api.PendingPrompt {
	return []api.PendingPrompt{
		{ID: "host-question", Prompt: askpass.Prompt{TunnelID: "db-prod", Kind: askpass.KindHostKey, Text: "Are you sure you want to continue connecting (yes/no)?"}},
		{ID: "password-question", Prompt: askpass.Prompt{TunnelID: "db-test", Kind: askpass.KindPassword, Text: "Password:"}},
	}
}

func TestPromptsList(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		if method == "prompt.list" {
			return promptFixture(), nil
		}
		return nil, api.NewError(api.Internal, "unexpected "+method)
	})
	for _, args := range [][]string{{"prompts"}, {"prompts", "--json"}} {
		code, out, diagnostics := lifecycleRun(context.Background(), args...)
		if code != 0 || diagnostics != "" || !strings.Contains(out, "host-question") || !strings.Contains(out, "db-test") || !strings.Contains(out, "Password:") {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
		if len(args) > 1 {
			var result struct {
				APIVersion int `json:"apiVersion"`
				Prompts    []api.PendingPrompt
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.APIVersion != api.Version || len(result.Prompts) != 2 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		}
	}
}

func TestPromptsListEmptyJSON(t *testing.T) {
	fakeDaemon(t, func(string, json.RawMessage) (any, *api.Error) { return nil, nil })
	if code, out, diagnostics := lifecycleRun(context.Background(), "prompts", "--json"); code != 0 || !strings.Contains(out, `"prompts":[]`) || diagnostics != "" {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, diagnostics)
	}
}

func TestPromptsAnswerStdinAndCancel(t *testing.T) {
	var mu sync.Mutex
	var answers []api.AnswerPromptParams
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		switch method {
		case "prompt.list":
			return promptFixture(), nil
		case "prompt.answer":
			var p api.AnswerPromptParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, api.NewError(api.Internal, err.Error())
			}
			mu.Lock()
			answers = append(answers, p)
			mu.Unlock()
			return api.Empty{}, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected "+method)
		}
	})
	for _, tc := range []struct {
		args         []string
		input, value string
		cancel       bool
	}{
		{args: []string{"prompts", "answer", "host-question", "--stdin"}, input: "yes\n", value: "yes"},
		{args: []string{"prompts", "answer", "password-question", "--stdin", "--no-input", "--json"}, input: "  secret value  \r\n", value: "  secret value  "},
		{args: []string{"prompts", "answer", "password-question", "--stdin"}, input: "no-newline", value: "no-newline"},
		{args: []string{"prompts", "answer", "host-question", "--cancel", "--json"}, cancel: true},
	} {
		var out, diagnostics bytes.Buffer
		code := Run(context.Background(), append([]string{"--no-daemon-start"}, tc.args...), IO{In: strings.NewReader(tc.input), Out: &out, Err: &diagnostics}, Options{})
		if code != 0 || diagnostics.Len() != 0 || strings.Contains(out.String(), "secret value") || strings.Contains(out.String(), "no-newline") {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", tc.args, code, &out, &diagnostics)
		}
		mu.Lock()
		answer := answers[len(answers)-1]
		mu.Unlock()
		if answer.ID != tc.args[2] || answer.Value != tc.value || answer.Cancelled != tc.cancel {
			t.Errorf("%v: answer=%+v", tc.args, answer)
		}
	}
}

func TestPromptsAnswerNeverTakesValueFlag(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		if method == "prompt.list" {
			return promptFixture(), nil
		}
		return nil, api.NewError(api.Internal, "unexpected "+method)
	})
	for _, args := range [][]string{
		{"prompts", "answer", "host-question", "--answer", "yes"},
		{"prompts", "answer", "host-question", "--value", "yes"},
		{"prompts", "answer", "host-question", "yes"},
		{"prompts", "answer", "host-question", "--stdin", "--cancel"},
		{"prompts", "answer", "unknown", "--stdin"},
	} {
		if code, out, diagnostics := lifecycleRun(context.Background(), args...); code != 2 || out != "" {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}

func TestPromptsAnswerNonTTYNeedsStdin(t *testing.T) {
	fakeDaemon(t, func(method string, _ json.RawMessage) (any, *api.Error) {
		if method == "prompt.list" {
			return promptFixture(), nil
		}
		return nil, api.NewError(api.Internal, "must not read or cancel: "+method)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if code, out, diagnostics := lifecycleRun(ctx, "prompts", "answer", "host-question", "--json"); code != 4 || out != "" || !strings.Contains(diagnostics, `"code":"needs_user"`) || ctx.Err() != nil {
		t.Fatalf("code=%d stdout=%s stderr=%s ctx=%v", code, out, diagnostics, ctx.Err())
	}
}

func TestPromptsCancelledContextCancelsBrokerQuestion(t *testing.T) {
	answered := make(chan api.AnswerPromptParams, 1)
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method == "prompt.answer" {
			var p api.AnswerPromptParams
			_ = json.Unmarshal(params, &p)
			answered <- p
			return api.Empty{}, nil
		}
		return nil, api.NewError(api.Internal, "unexpected "+method)
	})
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	a := newApp(IO{In: reader}, Options{})
	a.noDaemonStart = true
	defer a.close()
	ctx, cancel := context.WithCancel(context.Background())
	// Establish the connection before cancelling, as happens after prompt.list.
	if _, err := a.client(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.answerPendingPrompt(ctx, promptFixture()[0], true, false) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("answer did not unblock on cancellation")
	}
	select {
	case answer := <-answered:
		if answer.ID != "host-question" || !answer.Cancelled || answer.Value != "" {
			t.Fatalf("answer=%+v", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel was not delivered to daemon")
	}
}

func TestReadPromptLinePreservesAnswerAndDoesNotReadAhead(t *testing.T) {
	reader := strings.NewReader("  first answer \r\nsecond\n")
	first, err := readPromptLine(reader)
	if err != nil || first != "  first answer " {
		t.Fatalf("first=%q err=%v", first, err)
	}
	second, err := readPromptLine(reader)
	if err != nil || second != "second" {
		t.Fatalf("second=%q err=%v", second, err)
	}
	if _, err := readPromptLine(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF=%v", err)
	}
}
