package supervisor

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestWaitReadyTerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   State
		pending bool
		message string
		want    error
	}{
		{"running", StateRunning, false, "", nil},
		{"pending secret", StateStopped, true, "", ErrNeedsSecret},
		{"needs token", StateNeedsToken, false, "", ErrNeedsSecret},
		{"keyring unavailable", StateKeyringUnavailable, false, "", secret.ErrUnavailable},
		{"stopped", StateStopped, false, "", ErrStopped},
		{"failed", StateFailed, false, "spawn failed", errors.New("spawn failed")},
		{"empty failure", StateFailed, false, "", errors.New("tunnel failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(&fakeRunner{}, &secret.Memory{}, logReady("a", false))
			defer m.Shutdown()
			m.tunnels["a"].status = Status{ID: "a", State: tc.state, Error: tc.message}
			m.pendingStart["a"] = tc.pending
			st, err := m.WaitReady(context.Background(), "a")
			if st.State != tc.state {
				t.Fatalf("state = %s", st.State)
			}
			if tc.state == StateFailed {
				if err == nil || err.Error() != tc.want.Error() {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWaitReadyMissingSecretPreservesRetry(t *testing.T) {
	r := &fakeRunner{onStart: func(p *fakeProcess) { _, _ = io.WriteString(p.req.Output, "started tunnel url=https://example.com\n") }}
	store := &secret.Memory{}
	m := newManager(r, store, secretTunnel())
	defer m.Shutdown()
	if err := m.Start("n"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WaitReady(context.Background(), "n"); !errors.Is(err, ErrNeedsSecret) {
		t.Fatalf("error = %v", err)
	}
	if err := store.Set("tok", "value"); err != nil {
		t.Fatal(err)
	}
	m.SecretsChanged()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if st, err := m.WaitReady(ctx, "n"); err != nil || st.State != StateRunning {
		t.Fatalf("retried readiness = %#v, %v", st, err)
	}
	m.Stop("n")
}

func TestWaitReadyDuringStart(t *testing.T) {
	for _, action := range []string{"ready", "stop", "fail", "cancel"} {
		t.Run(action, func(t *testing.T) {
			r := &fakeRunner{}
			m := newManager(r, &secret.Memory{}, logReady("a", false))
			defer m.Shutdown()
			if err := m.Start("a"); err != nil {
				t.Fatal(err)
			}
			p := r.proc(t, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := m.WaitReady(ctx, "a"); result <- err }()
			eventually(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.waiters["a"]) == 1 }, "waiter registration")
			var want error
			switch action {
			case "ready":
				_, _ = io.WriteString(p.req.Output, "ready\n")
			case "stop":
				m.Stop("a")
				want = ErrStopped
			case "fail":
				p.finish(errors.New("process failed"))
			case "cancel":
				cancel()
				want = context.Canceled
			}
			select {
			case err := <-result:
				if action == "fail" {
					if err == nil || err.Error() == "" {
						t.Fatalf("error = %v", err)
					}
				} else if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wait did not complete")
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if len(m.waiters["a"]) != 0 {
				t.Fatal("waiter leaked")
			}
		})
	}
}
