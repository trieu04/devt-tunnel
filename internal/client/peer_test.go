package client

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func TestUntrustedDaemonReceivesNoHello(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "peer refusal", true: "shared directory"}[shared], func(t *testing.T) {
			if shared && runtime.GOOS == "windows" {
				t.Skip("Unix directory permissions")
			}
			dir, err := os.MkdirTemp("", "dtt-peer")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			if shared {
				if err = os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			socket := filepath.Join(dir, "ctl.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			received := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					received <- err
					return
				}
				defer conn.Close()
				conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				var b [1]byte
				n, err := conn.Read(b[:])
				if n != 0 {
					received <- errors.New("sent bytes before authentication")
					return
				}
				received <- err
			}()
			refused := errors.New("untrusted daemon peer")
			check := func(net.Conn) error { return refused }
			if shared {
				check = platform.CheckPeer
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, err := dialManagement(ctx, socket, api.ClientInfo{Kind: "cli", Name: "test"}, check)
			if c != nil {
				c.Close()
				t.Fatal("accepted untrusted daemon")
			}
			if err == nil || (!shared && !errors.Is(err, refused)) {
				t.Fatalf("authentication: %v", err)
			}
			select {
			case err := <-received:
				if !errors.Is(err, io.EOF) {
					t.Fatalf("peer did not see immediate EOF without bytes: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
