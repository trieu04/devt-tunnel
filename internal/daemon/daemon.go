// Package daemon owns the headless engine, control socket and process lifecycle.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/control"
	"github.com/trieu04/devt-tunnel/internal/engine"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

const DefaultIdleExit = 5 * time.Minute

type Options struct {
	Socket   string
	Version  string
	IdleExit time.Duration
	Engine   engine.Options
}

func Run(ctx context.Context, opts Options) error {
	if opts.IdleExit < 0 {
		return errors.New("idle exit must not be negative")
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, shutdownSignals()...)
	defer signal.Stop(signals)
	var err error
	if opts.Socket == "" {
		opts.Socket, err = platform.ControlSocket()
		if err != nil {
			return err
		}
	}
	dir := filepath.Dir(opts.Socket)
	if err = platform.PreparePrivateDir(dir); err != nil {
		return err
	}
	release, err := platform.AcquireLock(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return err
	}
	defer release()
	if opts.Engine.StateDir == "" {
		opts.Engine.StateDir, err = platform.StateDir()
		if err != nil {
			return err
		}
	}
	if err = platform.PreparePrivateDir(opts.Engine.StateDir); err != nil {
		return err
	}
	output, err := platform.OpenRotatingLogFile(opts.Engine.StateDir, "daemon.log")
	if err != nil {
		return err
	}
	defer output.Close()
	previous := log.Writer()
	log.SetOutput(output)
	defer log.SetOutput(previous)
	if info, err := os.Lstat(opts.Socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket %q", opts.Socket)
		}
		if err = os.Remove(opts.Socket); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	opts.Engine.Version = opts.Version
	e, err := engine.New(opts.Engine)
	if err != nil {
		return err
	}
	defer e.Shutdown()
	listener, err := net.Listen("unix", opts.Socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(opts.Socket, 0o600); err != nil {
		return err
	}
	shutdown := make(chan struct{})
	var once sync.Once
	s := control.New(e, opts.Version, func() { once.Do(func() { close(shutdown) }) })
	defer s.Close()
	served := make(chan error, 1)
	go func() { served <- s.Serve(listener) }()
	interval := time.Second
	if opts.IdleExit > 0 && opts.IdleExit/4 < interval {
		interval = max(opts.IdleExit/4, time.Millisecond)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	idleSince := time.Now()
	log.Printf("daemon ready pid=%d api=%d", os.Getpid(), s.Hello().APIVersion)
	for {
		select {
		case <-ctx.Done():
			e.Shutdown()
			return nil
		case <-shutdown:
			return nil
		case err := <-served:
			e.Shutdown()
			return err
		case sig := <-signals:
			if !isHangup(sig) {
				e.Shutdown()
				return nil
			}
		case now := <-ticker.C:
			if e.Active() || s.Connections() > 0 {
				idleSince = now
				continue
			}
			if opts.IdleExit > 0 && now.Sub(idleSince) >= opts.IdleExit {
				e.Shutdown()
				return nil
			}
		}
	}
}
