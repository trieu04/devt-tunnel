package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/profiles"
	"github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func (s *Engine) ActivateProfile(id string) error {
	cfg := s.manager.Config()
	var selected *model.Profile
	if id != "" {
		for n := range cfg.Profiles {
			if cfg.Profiles[n].ID == id {
				selected = &cfg.Profiles[n]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("unknown profile %q", id)
		}
	}
	running := map[string]bool{}
	for _, status := range s.manager.Statuses() {
		running[status.ID] = status.State == supervisor.StateRunning || status.State == supervisor.StateStarting || status.State == supervisor.StateReconnecting
	}
	if selected != nil {
		plan := profiles.Activation(cfg, *selected, running)
		for _, tunnelID := range plan.Stop {
			s.Stop(tunnelID)
		}
		for _, tunnelID := range plan.Start {
			_ = s.Start(tunnelID)
		}
	}
	// Serialize the selected SSH configuration with fingerprint authorization
	// and StartFingerprint; profile changes must not slip between the two builds.
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	if err := s.stateStore.Update(func(st *state.State) { st.ActiveProfile = id }); err != nil {
		return err
	}
	s.manager.SetActiveProfile(id)
	return nil
}

func (s *Engine) SaveProfile(profile model.Profile, members []string) (string, error) {
	err := s.updateConfig(func(cfg *model.Config) error {
		if profile.ID == "" {
			taken := map[string]bool{}
			for _, p := range cfg.Profiles {
				taken[p.ID] = true
			}
			profile.ID = model.NewID(profile.Name, taken)
			cfg.Profiles = append(cfg.Profiles, profile)
		} else {
			found := false
			for n := range cfg.Profiles {
				if cfg.Profiles[n].ID == profile.ID {
					cfg.Profiles[n] = profile
					found = true
				}
			}
			if !found {
				return fmt.Errorf("unknown profile %q", profile.ID)
			}
		}
		memberSet := map[string]bool{}
		for _, id := range members {
			memberSet[id] = true
		}
		for n := range cfg.Tunnels {
			cfg.Tunnels[n].Profiles = removeString(cfg.Tunnels[n].Profiles, profile.ID)
			if memberSet[cfg.Tunnels[n].ID] {
				cfg.Tunnels[n].Profiles = append(cfg.Tunnels[n].Profiles, profile.ID)
			}
		}
		validAutostart := profile.Autostart[:0]
		for _, id := range profile.Autostart {
			if memberSet[id] {
				validAutostart = append(validAutostart, id)
			}
		}
		for n := range cfg.Profiles {
			if cfg.Profiles[n].ID == profile.ID {
				cfg.Profiles[n].Autostart = validAutostart
			}
		}
		return nil
	})
	return profile.ID, err
}

func (s *Engine) DeleteProfile(id string) error {
	err := s.updateConfig(func(cfg *model.Config) error { profiles.Remove(cfg, id); return nil })
	if err != nil {
		return err
	}
	if s.stateStore.Get().ActiveProfile == id {
		return s.ActivateProfile("")
	}
	return nil
}

func (s *Engine) DuplicateProfile(id string) (string, error) {
	var duplicate model.Profile
	err := s.updateConfig(func(cfg *model.Config) error {
		duplicate = profiles.Duplicate(cfg, id)
		if duplicate.ID == "" {
			return fmt.Errorf("unknown profile %q", id)
		}
		return nil
	})
	return duplicate.ID, err
}

func (s *Engine) SetProfileAutostart(id, tunnelID string, on bool) error {
	err := s.updateConfig(func(cfg *model.Config) error {
		for n := range cfg.Profiles {
			if cfg.Profiles[n].ID == id {
				cfg.Profiles[n].Autostart = removeString(cfg.Profiles[n].Autostart, tunnelID)
				if on {
					cfg.Profiles[n].Autostart = append(cfg.Profiles[n].Autostart, tunnelID)
				}
				return nil
			}
		}
		return fmt.Errorf("unknown profile %q", id)
	})
	return err
}

func (s *Engine) StartProfile(id string) error {
	var failures []string
	for _, tunnel := range profiles.Members(s.manager.Config(), id) {
		if strings.HasPrefix(tunnel.ID, "tmp-") {
			continue
		}
		if err := s.Start(tunnel.ID); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", tunnel.Name, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (s *Engine) StopProfile(id string) {
	for _, tunnel := range profiles.Members(s.manager.Config(), id) {
		s.Stop(tunnel.ID)
	}
}
