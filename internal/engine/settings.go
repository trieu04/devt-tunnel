package engine

import "github.com/trieu04/devt-tunnel/internal/core/settings"

func (s *Engine) Shortcut() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.Shortcut
}

func (s *Engine) SetShortcut(accel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.Shortcut = accel
	return settings.Save(s.settingsPath, s.settings)
}

func (s *Engine) SetUpdateChecks(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.CheckUpdates = &enabled
	return settings.Save(s.settingsPath, s.settings)
}

func (s *Engine) UpdateChecksEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.CheckUpdates == nil || *s.settings.CheckUpdates
}
