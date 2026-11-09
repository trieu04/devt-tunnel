package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
)

func TestEnsureVersionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		api                                      int
		build                                    string
		active, noStart, wantShutdown, wantError bool
	}{
		{"same", api.Version, "ours", false, false, false, false},
		{"active build", api.Version, "older", true, false, false, false},
		{"idle build", api.Version, "older", false, false, true, false},
		{"active incompatible", api.Version + 1, "other", true, false, false, true},
		{"idle incompatible", api.Version + 1, "other", false, false, true, false},
		{"no start compatible", api.Version, "older", false, true, false, false},
		{"no start incompatible", api.Version + 1, "other", false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "dttv")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "ctl.sock")
			l, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var shutdown atomic.Bool
			go func() {
				for {
					conn, err := l.Accept()
					if err != nil {
						return
					}
					go func() {
						defer conn.Close()
						stop := context.AfterFunc(ctx, func() { conn.Close() })
						defer stop()
						dec, enc := json.NewDecoder(conn), json.NewEncoder(conn)
						for {
							var req api.Request
							if dec.Decode(&req) != nil {
								return
							}
							var result any
							hello := api.HelloResult{APIVersion: tc.api, DaemonVersion: tc.build, PID: 123}
							if shutdown.Load() {
								hello.APIVersion = api.Version
								hello.DaemonVersion = "ours"
							}
							switch req.Method {
							case api.DaemonHello:
								result = hello
							case api.DaemonStatusMethod:
								result = api.DaemonStatus{HelloResult: hello, Active: tc.active}
							case api.DaemonShutdown:
								var p api.ShutdownParams
								json.Unmarshal(req.Params, &p)
								if !p.OnlyIfIdle {
									t.Error("upgrade omitted idle guard")
								}
								shutdown.Store(true)
								result = api.Empty{}
							default:
								t.Errorf("unexpected method %s", req.Method)
							}
							raw, _ := json.Marshal(result)
							if enc.Encode(api.Response{JSONRPC: "2.0", ID: req.ID, Result: raw}) != nil {
								return
							}
							if req.Method == api.DaemonShutdown {
								return
							}
						}
					}()
				}
			}()
			c, err := Ensure(ctx, EnsureOptions{Socket: socket, Info: api.ClientInfo{Kind: "cli", Name: "test", Version: "ours"}, NoStart: tc.noStart})
			if c != nil {
				c.Close()
			}
			if tc.wantError {
				var rpc *api.Error
				if !errors.As(err, &rpc) || rpc.Data.Kind != api.Incompatible {
					t.Fatalf("expected incompatible: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if shutdown.Load() != tc.wantShutdown {
				t.Fatalf("shutdown=%t", shutdown.Load())
			}
		})
	}
}

func TestEnsureNoStartAndCancellation(t *testing.T) {
	dir, err := os.MkdirTemp("", "dttv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	opts := EnsureOptions{Socket: filepath.Join(dir, "absent.sock"), NoStart: true}
	if c, err := Ensure(context.Background(), opts); err == nil {
		c.Close()
		t.Fatal("connected to absent socket")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Ensure(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDaemonPathAdjacentThenPATH(t *testing.T) {
	name := "dtt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := t.TempDir()
	fallback := t.TempDir()
	adjacent := filepath.Join(dir, name)
	onPath := filepath.Join(fallback, name)
	if err := os.WriteFile(onPath, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fallback)
	got, err := daemonPath(filepath.Join(dir, "gui"))
	if err != nil || got != onPath {
		t.Fatalf("PATH: %q %v", got, err)
	}
	if err = os.WriteFile(adjacent, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = daemonPath(filepath.Join(dir, "gui"))
	if err != nil || got != adjacent {
		t.Fatalf("adjacent: %q %v", got, err)
	}
}

// Model the ordering where the socket's deadline has elapsed but the context
// timer goroutine has not published Err/Done yet. No scheduler timing is needed.
type unpublishedDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c unpublishedDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }

// Let Call write its request before the reader observes the expired socket
// deadline; otherwise closing the connection could race SetWriteDeadline.
type readTimeoutConn struct {
	net.Conn
	written chan struct{}
}

func (c readTimeoutConn) Write(p []byte) (int, error) {
	close(c.written)
	return len(p), nil
}

func TestCallSocketDeadlineBeforeContextTimer(t *testing.T) {
	for _, direction := range []string{"write", "read"} {
		t.Run(direction, func(t *testing.T) {
			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			ctx := unpublishedDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
			c := &Client{conn: conn, done: make(chan struct{}), pending: map[string]chan api.Response{}}
			defer c.Close()
			if direction == "read" {
				if err := conn.SetReadDeadline(ctx.deadline); err != nil {
					t.Fatal(err)
				}
				written := make(chan struct{})
				c.conn = readTimeoutConn{Conn: conn, written: written}
				go func() {
					<-written
					c.read()
				}()
			}
			if err := c.Call(ctx, "fixture", api.Empty{}, nil); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("socket %s timeout must be a context deadline error: %T %v", direction, err, err)
			}
			if ctx.Err() != nil {
				t.Fatal("fixture must leave context cancellation unpublished")
			}
		})
	}
}

func TestContextIOErrorKeepsIndependentTimeouts(t *testing.T) {
	timeout := &net.DNSError{Err: "fixture timeout", IsTimeout: true}
	future := unpublishedDeadlineContext{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
	for _, ctx := range []context.Context{context.Background(), future} {
		if err := contextIOError(ctx, timeout); err != timeout {
			t.Fatalf("independent timeout changed to %v", err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := contextIOError(canceled, timeout); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled I/O timeout = %v", err)
	}
	transport := errors.New("fixture transport failure")
	if err := contextIOError(canceled, transport); err != transport {
		t.Fatalf("non-timeout transport error changed to %v", err)
	}
}
