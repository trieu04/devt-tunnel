package engine

import (
	"maps"

	"github.com/trieu04/devt-tunnel/internal/core/settings"
)

func (s *Engine) Shortcut() string {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.settings.Shortcut
}

func (s *Engine) updateSettings(change func(*settings.Settings)) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if err := s.lifetimeError(); err != nil {
		return err
	}
	next := s.settings
	next.Tools = maps.Clone(next.Tools)
	change(&next)
	err := s.saveSettings(s.settingsPath, next)
	if stopped := s.lifetimeError(); stopped != nil {
		return stopped
	}
	if err != nil {
		return err
	}
	return s.applyWhileAlive(func() {
		s.settings = next
		s.tools.SetOverrides(next.Tools)
	})
}

func (s *Engine) SetShortcut(accel string) error {
	return s.updateSettings(func(next *settings.Settings) { next.Shortcut = accel })
}

func (s *Engine) SetUpdateChecks(enabled bool) error {
	return s.updateSettings(func(next *settings.Settings) { next.CheckUpdates = &enabled })
}

func (s *Engine) UpdateChecksEnabled() bool {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.settings.CheckUpdates == nil || *s.settings.CheckUpdates
}
