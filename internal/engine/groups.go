package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// SaveGroup stores the group and makes members exactly its tunnels.
func (s *Engine) SaveGroup(group model.Group, members []string) (string, error) {
	err := s.updateConfig(func(cfg *model.Config) error {
		if group.ID == "" {
			taken := map[string]bool{}
			for _, g := range cfg.Groups {
				taken[g.ID] = true
			}
			group.ID = model.NewID(group.Name, taken)
			cfg.Groups = append(cfg.Groups, group)
		} else {
			n := slices.IndexFunc(cfg.Groups, func(g model.Group) bool { return g.ID == group.ID })
			if n < 0 {
				return fmt.Errorf("unknown group %q", group.ID)
			}
			cfg.Groups[n] = group
		}
		memberSet := stringSet(members)
		for n := range cfg.Tunnels {
			has := slices.Contains(cfg.Tunnels[n].Groups, group.ID)
			if memberSet[cfg.Tunnels[n].ID] && !has {
				cfg.Tunnels[n].Groups = append(cfg.Tunnels[n].Groups, group.ID)
			} else if !memberSet[cfg.Tunnels[n].ID] && has {
				cfg.Tunnels[n].Groups = removeString(cfg.Tunnels[n].Groups, group.ID)
			}
		}
		return nil
	})
	return group.ID, err
}

// DeleteGroup keeps the tunnels and leaves them running.
func (s *Engine) DeleteGroup(id string) error {
	return s.updateConfig(func(cfg *model.Config) error {
		cfg.Groups = slices.DeleteFunc(cfg.Groups, func(g model.Group) bool { return g.ID == id })
		for n := range cfg.Tunnels {
			cfg.Tunnels[n].Groups = removeString(cfg.Tunnels[n].Groups, id)
		}
		return nil
	})
}

// DuplicateGroup copies the settings and the membership.
func (s *Engine) DuplicateGroup(id string) (string, error) {
	var duplicate model.Group
	err := s.updateConfig(func(cfg *model.Config) error {
		taken := map[string]bool{}
		for _, group := range cfg.Groups {
			taken[group.ID] = true
			if group.ID == id {
				duplicate = group
			}
		}
		if duplicate.ID == "" {
			return fmt.Errorf("unknown group %q", id)
		}
		duplicate.Name = strings.TrimSpace(duplicate.Name) + " copy"
		duplicate.ID = model.NewID(duplicate.Name, taken)
		cfg.Groups = append(cfg.Groups, duplicate)
		for n := range cfg.Tunnels {
			if slices.Contains(cfg.Tunnels[n].Groups, id) {
				cfg.Tunnels[n].Groups = append(cfg.Tunnels[n].Groups, duplicate.ID)
			}
		}
		return nil
	})
	return duplicate.ID, err
}

// StartGroup starts every saved member and reports all failures together, so
// one missing secret does not hold back the others.
func (s *Engine) StartGroup(id string) error {
	var failures []string
	for _, tunnel := range s.groupMembers(id) {
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

func (s *Engine) StopGroup(id string) {
	for _, tunnel := range s.groupMembers(id) {
		s.Stop(tunnel.ID)
	}
}

func (s *Engine) groupMembers(id string) []model.Tunnel {
	var members []model.Tunnel
	for _, tunnel := range s.manager.Config().Tunnels {
		if slices.Contains(tunnel.Groups, id) {
			members = append(members, tunnel)
		}
	}
	return members
}
