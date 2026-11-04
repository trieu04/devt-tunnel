package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type EnsureOptions struct {
	Socket     string
	Info       api.ClientInfo
	DaemonPath string
	NoStart    bool
	DaemonArgs []string
}

func DaemonPath() (string, error) {
	exe, _ := os.Executable()
	return daemonPath(exe)
}

func daemonPath(exe string) (string, error) {
	name := "dtt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe != "" {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		path := filepath.Join(filepath.Dir(exe), name)
		if found, err := exec.LookPath(path); err == nil {
			return found, nil
		}
	}
	return exec.LookPath(name)
}

func Ensure(ctx context.Context, opts EnsureOptions) (*Client, error) {
	if opts.Socket == "" {
		var err error
		opts.Socket, err = platform.ControlSocket()
		if err != nil {
			return nil, err
		}
	}
	started := false
	var exited <-chan struct{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("waiting for daemon: %w", err)
		}
		c, err := DialManagement(ctx, opts.Socket, opts.Info)
		if err == nil {
			mismatch := c.hello.APIVersion != api.Version
			if !mismatch && c.hello.DaemonVersion == opts.Info.Version {
				return c, nil
			}
			var status api.DaemonStatus
			err = c.Call(ctx, api.DaemonStatusMethod, api.Empty{}, &status)
			if err == nil && status.Active {
				if mismatch {
					c.Close()
					return nil, api.VersionError(c.hello.APIVersion)
				}
				return c, nil
			}
			if err == nil && opts.NoStart {
				if mismatch {
					c.Close()
					return nil, api.VersionError(c.hello.APIVersion)
				}
				return c, nil
			}
			if err == nil {
				err = c.Call(ctx, api.DaemonShutdown, api.ShutdownParams{OnlyIfIdle: true}, nil)
			}
			if err == nil {
				select {
				case <-c.Done():
				case <-ctx.Done():
					c.Close()
					return nil, ctx.Err()
				}
			}
			c.Close()
			var rpc *api.Error
			if err != nil && !TransportError(err) && !(errors.As(err, &rpc) && (rpc.Data.Kind == api.Conflict || rpc.Data.Kind == api.Stopped)) {
				return nil, err
			}
		} else {
			if opts.NoStart {
				return nil, err
			}
			if !TransportError(err) {
				return nil, err
			}
			if started {
				select {
				case <-exited:
					started = false
				default:
				}
			}
			if !started {
				path := opts.DaemonPath
				if path == "" {
					path, err = DaemonPath()
					if err != nil {
						return nil, err
					}
				}
				cmd := exec.Command(path, append([]string{"daemon", "run"}, opts.DaemonArgs...)...)
				cmd.Env = append(os.Environ(), "DTT_SOCKET="+opts.Socket)
				platform.Detach(cmd)
				if err = cmd.Start(); err != nil {
					return nil, err
				}
				done := make(chan struct{})
				exited = done
				go func() { _ = cmd.Wait(); close(done) }()
				started = true
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// TransportError reports connection failures that can succeed on a fresh daemon connection.
func TransportError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}
