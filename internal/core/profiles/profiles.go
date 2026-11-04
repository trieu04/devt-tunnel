package profiles

import (
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

type Plan struct {
	Start []string
	Stop  []string
}

func Activation(cfg model.Config, p model.Profile, running map[string]bool) Plan {
	var plan Plan
	members := map[string]bool{}
	for _, tunnel := range cfg.Tunnels {
		if has(tunnel.Profiles, p.ID) {
			members[tunnel.ID] = true
		}
	}
	if p.OnActivate == "start_autostart" {
		for _, id := range p.Autostart {
			if members[id] && !running[id] {
				plan.Start = append(plan.Start, id)
			}
		}
	}
	if p.StopNonMembers {
		for _, tunnel := range cfg.Tunnels {
			if running[tunnel.ID] && !members[tunnel.ID] {
				plan.Stop = append(plan.Stop, tunnel.ID)
			}
		}
	}
	return plan
}

func Members(cfg model.Config, profileID string) []model.Tunnel {
	var out []model.Tunnel
	for _, tunnel := range cfg.Tunnels {
		if has(tunnel.Profiles, profileID) {
			out = append(out, tunnel)
		}
	}
	return out
}

func Remove(cfg *model.Config, profileID string) {
	profiles := cfg.Profiles[:0]
	for _, profile := range cfg.Profiles {
		if profile.ID != profileID {
			profiles = append(profiles, profile)
		}
	}
	cfg.Profiles = profiles
	for n := range cfg.Tunnels {
		cfg.Tunnels[n].Profiles = without(cfg.Tunnels[n].Profiles, profileID)
	}
}

func Duplicate(cfg *model.Config, profileID string) model.Profile {
	for _, profile := range cfg.Profiles {
		if profile.ID != profileID {
			continue
		}
		taken := map[string]bool{}
		for _, candidate := range cfg.Profiles {
			taken[candidate.ID] = true
		}
		copy := profile
		copy.ID = model.NewID(profile.Name+" copy", taken)
		copy.Name = strings.TrimSpace(profile.Name) + " copy"
		copy.Autostart = append([]string(nil), profile.Autostart...)
		cfg.Profiles = append(cfg.Profiles, copy)
		for n := range cfg.Tunnels {
			if has(cfg.Tunnels[n].Profiles, profileID) {
				cfg.Tunnels[n].Profiles = append(cfg.Tunnels[n].Profiles, copy.ID)
			}
		}
		return copy
	}
	return model.Profile{}
}

func has(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func without(values []string, value string) []string {
	out := values[:0]
	for _, candidate := range values {
		if candidate != value {
			out = append(out, candidate)
		}
	}
	return out
}
