package engine

import (
	"fmt"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// SaveProfile stores the profile and makes members exactly its tunnels. A member
// that was in another profile moves here, since a tunnel has only one.
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
		memberSet := stringSet(members)
		for n := range cfg.Tunnels {
			if memberSet[cfg.Tunnels[n].ID] {
				cfg.Tunnels[n].Profile = profile.ID
			} else if cfg.Tunnels[n].Profile == profile.ID {
				cfg.Tunnels[n].Profile = ""
			}
		}
		return nil
	})
	return profile.ID, err
}

// DeleteProfile keeps the tunnels, which fall back to the default SSH config.
func (s *Engine) DeleteProfile(id string) error {
	return s.updateConfig(func(cfg *model.Config) error {
		profiles := cfg.Profiles[:0]
		for _, profile := range cfg.Profiles {
			if profile.ID != id {
				profiles = append(profiles, profile)
			}
		}
		cfg.Profiles = profiles
		for n := range cfg.Tunnels {
			if cfg.Tunnels[n].Profile == id {
				cfg.Tunnels[n].Profile = ""
			}
		}
		return nil
	})
}

// DuplicateProfile copies the settings only: its tunnels cannot be in two profiles.
func (s *Engine) DuplicateProfile(id string) (string, error) {
	var duplicate model.Profile
	err := s.updateConfig(func(cfg *model.Config) error {
		taken := map[string]bool{}
		for _, profile := range cfg.Profiles {
			taken[profile.ID] = true
			if profile.ID == id {
				duplicate = profile
			}
		}
		if duplicate.ID == "" {
			return fmt.Errorf("unknown profile %q", id)
		}
		duplicate.Name = strings.TrimSpace(duplicate.Name) + " copy"
		duplicate.ID = model.NewID(duplicate.Name, taken)
		cfg.Profiles = append(cfg.Profiles, duplicate)
		return nil
	})
	return duplicate.ID, err
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
