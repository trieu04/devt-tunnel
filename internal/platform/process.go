package platform

import (
	"errors"
	"os/exec"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

// outputDrain is how long to wait for output after exit. A grandchild that keeps
// the pipe open must not hold up the exit report.
const outputDrain = 2 * time.Second

// Runner starts real processes.
type Runner struct{}

type process struct {
	cmd      *exec.Cmd
	identity string
	done     chan struct{}
	waitErr  error
	os       osProcess
}

func (Runner) Start(req supervisor.SpawnRequest) (supervisor.Process, error) {
	cmd := exec.Command(req.Path, req.Args...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.Stdout = req.Output
	cmd.Stderr = req.Output
	cmd.WaitDelay = outputDrain
	configure(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	p.os = attach(cmd)
	p.identity, _ = ProcessIdentity(cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		p.waitErr = err
		p.os.exited()
		close(p.done)
	}()
	return p, nil
}

func (p *process) PID() int         { return p.cmd.Process.Pid }
func (p *process) Identity() string { return p.identity }

func (p *process) Wait() error {
	<-p.done
	return p.waitErr
}

func (p *process) Stop(grace time.Duration) error {
	err := p.os.terminate()
	select {
	case <-p.done:
		return err
	case <-time.After(grace):
	}
	err = p.os.kill()
	<-p.done
	return err
}
