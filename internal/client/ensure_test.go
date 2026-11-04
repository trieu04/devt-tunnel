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
		{"same", 1, "ours", false, false, false, false},
		{"active build", 1, "older", true, false, false, false},
		{"idle build", 1, "older", false, false, true, false},
		{"active incompatible", 2, "other", true, false, false, true},
		{"idle incompatible", 2, "other", false, false, true, false},
		{"no start compatible", 1, "older", false, true, false, false},
		{"no start incompatible", 2, "other", false, true, false, true},
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
