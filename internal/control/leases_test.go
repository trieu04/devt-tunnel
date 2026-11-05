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

type leaseClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *leaseClock) Now() time.Time                         { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *leaseClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (c *leaseClock) advance(d time.Duration)                { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type leaseProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *leaseProcess) PID() int                 { return 123 }
func (p *leaseProcess) Identity() string         { return "fake" }
func (p *leaseProcess) Wait() error              { <-p.done; return nil }
func (p *leaseProcess) Stop(time.Duration) error { p.once.Do(func() { close(p.done) }); return nil }

type leaseRunner struct{}

func (leaseRunner) Start(r supervisor.SpawnRequest) (supervisor.Process, error) {
	p := &leaseProcess{done: make(chan struct{})}
	go func() { _, _ = io.WriteString(r.Output, "ready\n") }()
	return p, nil
}
func leasedServer(t *testing.T) (*Server, string, *leaseClock) {
	t.Helper()
	clock := &leaseClock{now: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)}
	s, socket := testServerWithOptions(t, func(o *engine.Options) { o.Clock = clock; o.Runner = leaseRunner{} })
	return s, socket, clock
}
func leasedDial(t *testing.T, socket, kind, name, session string) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, socket, api.ClientInfo{Kind: kind, Name: name, Session: session})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Drain live claims before removing temporary state. Production disconnect
		// cleanup is intentionally asynchronous so a blocked store cannot hang exit.
		if kind == "mcp" {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var leases []api.Lease
			if c.Call(ctx, "lease.list", api.Empty{}, &leases) == nil {
				for _, l := range leases {
					if !l.Revoked {
						_ = c.Call(ctx, "lease.close", api.IDParams{ID: l.ID}, nil)
					}
				}
			}
		}
		c.Close()
	})
	return c
}
func rpcLease(t *testing.T, c *client.Client) api.Lease {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var l api.Lease
	err := c.Call(ctx, "lease.open", api.OpenLeaseParams{ID: "t"}, &l)
	var rpc *api.Error
	if errors.As(err, &rpc) && rpc.Data.Kind == api.PendingApproval {
		// This legacy fixture is a human-authored command tunnel: approve it before
		// exercising the independent lease/reconnect semantics below.
		person := leasedDial(t, os.Getenv("DTT_SOCKET"), "cli", "person", "")
		var a api.Approval
		if err := person.Call(ctx, "approval.get", api.IDParams{ID: rpc.Data.ApprovalID}, &a); err != nil {
			t.Fatal(err)
		}
		if err := person.Call(ctx, "approval.decide", api.DecideApprovalParams{ID: a.ID, Decision: "approve", Scope: "once", Code: a.Code}, nil); err != nil {
			t.Fatal(err)
		}
		err = c.Call(ctx, "lease.open", api.OpenLeaseParams{ID: "t"}, &l)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "tunnel.waitReady", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	return l
}
func waitForGrace(t *testing.T, s *Server, id string, want bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		leases := s.engine.ListLeases(api.Actor{}, "", false)
		for _, l := range leases {
			if l.ID == id && (l.GraceUntil != nil) == want {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("grace=%v not observed: %+v", want, leases)
		case <-ticker.C:
		}
	}
}
func assertRunning(t *testing.T, s *Server, want bool) {
	t.Helper()
	statuses := s.engine.Statuses()
	got := len(statuses) > 0 && statuses[0].State == supervisor.StateRunning
	if got != want {
		t.Fatalf("running=%v want=%v: %+v", got, want, statuses)
	}
}

func TestConnectionLeasesGraceAndSessionReconnect(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "drop expires", true: "same token resumes"}[reconnect], func(t *testing.T) {
			s, socket, clock := leasedServer(t)
			c := leasedDial(t, socket, "mcp", "adapter", "")
			l := rpcLease(t, c)
			token := c.Session()
			if token == "" || l.Session != token {
				t.Fatalf("session=%q lease=%+v", token, l)
			}
			c.Close()
			waitForGrace(t, s, l.ID, true)
			clock.advance(engine.DisconnectGrace - time.Nanosecond)
			if err := s.engine.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			assertRunning(t, s, true)
			if reconnect {
				resumed := leasedDial(t, socket, "mcp", "adapter", token)
				if resumed.Session() != token {
					t.Fatal("session changed")
				}
				waitForGrace(t, s, l.ID, false)
			}
			clock.advance(time.Nanosecond)
			if err := s.engine.ExpireLeases(); err != nil {
				t.Fatal(err)
			}
			assertRunning(t, s, reconnect)
		})
	}
}
func TestConnectionDifferentTokenDoesNotResumeByName(t *testing.T) {
	s, socket, clock := leasedServer(t)
	c := leasedDial(t, socket, "mcp", "adapter", "")
	l := rpcLease(t, c)
	token := c.Session()
	c.Close()
	waitForGrace(t, s, l.ID, true)
	other := leasedDial(t, socket, "mcp", "adapter", "")
	if other.Session() == token {
		t.Fatal("different connection reused token implicitly")
	}
	clock.advance(engine.DisconnectGrace)
	if err := s.engine.ExpireLeases(); err != nil {
		t.Fatal(err)
	}
	assertRunning(t, s, false)
}
func TestConnectionOverlappingReconnectKeepsLeaseUntilLastDisconnect(t *testing.T) {
	s, socket, _ := leasedServer(t)
	first := leasedDial(t, socket, "mcp", "adapter", "")
	l := rpcLease(t, first)
	second := leasedDial(t, socket, "mcp", "adapter", first.Session())
	first.Close()
	// Server connection accounting confirms first.serve returned; its asynchronous
	// disconnect may follow, but cannot grace a session with another live socket.
	if _, err := s.engine.RenewLease(api.Actor{Kind: "mcp", Name: "adapter"}, second.Session(), api.RenewLeaseParams{ID: l.ID}); err != nil {
		t.Fatal(err)
	}
	second.Close()
	waitForGrace(t, s, l.ID, true)
}
func TestMCPLeasesCannotStopRevokeOrReleaseAnotherClient(t *testing.T) {
	s, socket, _ := leasedServer(t)
	first := leasedDial(t, socket, "mcp", "first", "")
	l := rpcLease(t, first)
	second := leasedDial(t, socket, "mcp", "second", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, method := range []string{"tunnel.stop", "tunnel.start", "tunnel.restart", "tunnel.startOnLaunch", "tunnel.delete", "tunnel.save", "tunnel.promote", "orphan.stop", api.DaemonShutdown, "profile.stop", "profile.start", "profile.activate", "lease.revoke", "lease.close", "lease.renew"} {
		id := "t"
		if method == "lease.revoke" || method == "lease.close" || method == "lease.renew" {
			id = l.ID
		}
		if err := second.Call(ctx, method, api.IDParams{ID: id}, nil); err == nil {
			t.Fatalf("MCP unexpectedly permitted %s", method)
		}
		assertRunning(t, s, true)
	}
	var own []api.Lease
	if err := second.Call(ctx, "lease.list", api.Empty{}, &own); err != nil || len(own) != 0 {
		t.Fatalf("other holder leases leaked: %+v %v", own, err)
	}
	assertRunning(t, s, true)
	person := leasedDial(t, socket, "cli", "person", "")
	if err := person.Call(ctx, "tunnel.stop", api.IDParams{ID: "t"}, nil); err != nil {
		t.Fatal(err)
	}
	err := first.Call(ctx, "lease.renew", api.RenewLeaseParams{ID: l.ID}, nil)
	var rpc *api.Error
	if !errors.As(err, &rpc) || rpc.Data.Kind != api.LeaseRevoked {
		t.Fatalf("person stop result=%v", err)
	}
	assertRunning(t, s, false)
}
