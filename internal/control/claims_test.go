package control

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/engine"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time                         { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (c *testClock) advance(d time.Duration)                { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type leaseProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *leaseProcess) PID() int                 { return 123 }
func (p *leaseProcess) Identity() string         { return "fake" }
func (p *leaseProcess) Wait() error              { <-p.done; return nil }
func (p *leaseProcess) Stop(time.Duration) error { p.once.Do(func() { close(p.done) }); return nil }

type fakeRunner struct{}

func (fakeRunner) Start(r supervisor.SpawnRequest) (supervisor.Process, error) {
	p := &leaseProcess{done: make(chan struct{})}
	go func() { _, _ = io.WriteString(r.Output, "ready\n") }()
	return p, nil
}
func claimServer(t *testing.T) (*Server, string, *testClock) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	s, socket := testServerWithOptions(t, func(o *engine.Options) { o.Clock = clock; o.Runner = fakeRunner{} })
	return s, socket, clock
}
func dialClient(t *testing.T, socket, kind, name, session string) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: kind, Name: name, Session: session})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Drain live holds before removing temporary state. Production disconnect
		// cleanup is intentionally asynchronous so a blocked store cannot hang exit.
		if kind == "mcp" {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var views []api.TunnelView
			if c.Call(ctx, "tunnel.list", api.Empty{}, &views) == nil {
				for _, v := range views {
					if v.Owner.Holder != nil {
						_ = c.Call(ctx, "tunnel.release", api.IDParams{ID: v.Tunnel.ID}, nil)
					}
				}
			}
		}
		c.Close()
	})
	return c
}
func rpcOpen(t *testing.T, c *client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id string
	err := c.Call(ctx, "tunnel.open", api.OpenSavedParams{ID: "t"}, &id)
	var rpc *api.Error
	if errors.As(err, &rpc) && rpc.Data.Kind == api.PendingApproval {
		// This legacy fixture is a human-authored command tunnel: approve it before
		// exercising the independent hold/reconnect semantics below.
		person := dialClient(t, os.Getenv("DTT_SOCKET"), "cli", "person", "")
		var a api.Approval
		if err := person.Call(ctx, "approval.get", api.IDParams{ID: rpc.Data.ApprovalID}, &a); err != nil {
			t.Fatal(err)
		}
		if err := person.Call(ctx, "approval.decide", api.DecideApprovalParams{ID: a.ID, Decision: "approve", Scope: "once", Code: a.Code}, nil); err != nil {
			t.Fatal(err)
		}
		err = c.Call(ctx, "tunnel.open", api.OpenSavedParams{ID: "t"}, &id)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
}
func isRunning(s *Server) bool {
	statuses := s.engine.Statuses()
	return len(statuses) > 0 && statuses[0].State == supervisor.StateRunning
}
func assertRunning(t *testing.T, s *Server, want bool) {
	t.Helper()
	if got := isRunning(s); got != want {
		t.Fatalf("running=%v want=%v: %+v", got, want, s.engine.Statuses())
	}
}

// Disconnect cleanup runs asynchronously after the socket closes.
func waitRunning(t *testing.T, s *Server, want bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for isRunning(s) != want {
		select {
		case <-deadline:
			t.Fatalf("running=%v not observed: %+v", want, s.engine.Statuses())
		case <-ticker.C:
		}
	}
}

func TestConnectionDropReleasesTunnel(t *testing.T) {
	s, socket, _ := claimServer(t)
	c := dialClient(t, socket, "cli", "adapter", "")
	rpcOpen(t, c)
	token := c.Session()
	if token == "" {
		t.Fatal("no session token")
	}
	c.Close()
	waitRunning(t, s, false)
	// Resuming the old token does not bring the hold back.
	resumed := dialClient(t, socket, "cli", "adapter", token)
	if resumed.Session() != token {
		t.Fatal("session changed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := resumed.Call(ctx, "tunnel.release", api.IDParams{ID: "t"}, nil); err == nil {
		t.Fatal("released a tunnel that was already released")
	}
	assertRunning(t, s, false)
}
func TestConnectionOverlappingReconnectKeepsTunnelUntilLastDisconnect(t *testing.T) {
	s, socket, _ := claimServer(t)
	first := dialClient(t, socket, "cli", "adapter", "")
	rpcOpen(t, first)
	second := dialClient(t, socket, "cli", "adapter", first.Session())
	first.Close()
	// Server connection accounting confirms first.serve returned; its asynchronous
	// disconnect may follow, but cannot release a session with another live socket.
	time.Sleep(50 * time.Millisecond)
	assertRunning(t, s, true)
	second.Close()
	waitRunning(t, s, false)
}
func TestMCPCannotStopOrManageAnotherClientsTunnel(t *testing.T) {
	s, socket, _ := claimServer(t)
	first := dialClient(t, socket, "mcp", "first", "")
	rpcOpen(t, first)
	second := dialClient(t, socket, "mcp", "second", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, method := range []string{"tunnel.stop", "tunnel.start", "tunnel.restart", "tunnel.startOnLaunch", "tunnel.delete", "tunnel.save", "tunnel.promote", "orphan.stop", api.DaemonShutdown, "profile.stop", "profile.start", "profile.activate"} {
		if err := second.Call(ctx, method, api.IDParams{ID: "t"}, nil); err == nil {
			t.Fatalf("MCP unexpectedly permitted %s", method)
		}
		assertRunning(t, s, true)
	}
	var views []api.TunnelView
	if err := second.Call(ctx, "tunnel.list", api.Empty{}, &views); err != nil || len(views) != 1 || views[0].Owner.Holder == nil || views[0].Owner.Holder.Name != "first" {
		t.Fatalf("agents should see which client holds a tunnel: %+v %v", views, err)
	}
	assertRunning(t, s, true)
	person := dialClient(t, socket, "cli", "person", "")
	if err := person.Call(ctx, "tunnel.stop", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := first.Call(ctx, "tunnel.release", api.IDParams{ID: "t"}, nil); err == nil {
		t.Fatal("agent released a tunnel that a person already stopped")
	}
	assertRunning(t, s, false)
}
