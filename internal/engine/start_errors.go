package engine

import (
	"fmt"
	"os/exec"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func invalidSpec(err error) error {
	if err == nil {
		return nil
	}
	return api.NewError(api.InvalidSpec, err.Error())
}

// Build exactly the execution spec and resolve its tool before approval/spawn.
// Locate permits configured paths, so LookPath also verifies such an override
// still names an executable. No request/error-text inference is involved.
func (s *Engine) buildStartDraft(t model.Tunnel) (command.Spec, error) {
	spec, err := s.manager.BuildDraft(t)
	if err != nil {
		return command.Spec{}, invalidSpec(err)
	}
	path, err := s.tools.Resolve(spec.Tool)
	if err == nil {
		_, err = exec.LookPath(path)
	}
	if err != nil {
		return command.Spec{}, api.NewError(api.NotFound, fmt.Sprintf("tool %q was not found on this machine", spec.Tool))
	}
	return spec, nil
}

func (s *Engine) startPortsAvailable(spec command.Spec) error {
	for _, address := range spec.Ready.Ports {
		if s.listening(command.DialAddress(address)) {
			return api.NewError(api.PortInUse, fmt.Sprintf("listen address %q is already in use", address))
		}
	}
	return nil
}

func (s *Engine) startPreflight(id string, ports bool) error {
	for _, t := range s.manager.Config().Tunnels {
		if t.ID != id {
			continue
		}
		spec, err := s.buildStartDraft(t)
		if err != nil {
			return err
		}
		if ports {
			return s.startPortsAvailable(spec)
		}
		return nil
	}
	return api.NewError(api.Unknown, "unknown tunnel")
}
