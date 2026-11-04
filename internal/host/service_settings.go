package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
)

type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
}

func (s *Service) SetShortcut(accel string) error {
	if s.host == nil {
		return errors.New("desktop is not attached")
	}
	if err := s.host.setShortcut(accel); err != nil {
		return err
	}
	if err := action(s, "settings.setShortcut", api.ShortcutParams{Shortcut: accel}); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings.Shortcut = accel
	s.mu.Unlock()
	return nil
}

func (s *Service) SetUpdateChecks(enabled bool) error {
	if err := action(s, "settings.setUpdateChecks", api.UpdateChecksParams{Enabled: enabled}); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings.UpdateChecks = enabled
	s.mu.Unlock()
	return nil
}

func (s *Service) UpdateChecksEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.UpdateChecks
}

func (s *Service) CheckForUpdate(force bool) (UpdateInfo, error) {
	s.mu.Lock()
	if !s.settings.UpdateChecks && !force {
		info := UpdateInfo{Current: Version}
		s.mu.Unlock()
		return info, nil
	}
	if !force && time.Since(s.updateChecked) < 24*time.Hour {
		info := s.updateInfo
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/trieu04/devt-tunnel/releases/latest", nil)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "devt-tunnel/"+Version)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return UpdateInfo{Current: Version}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return UpdateInfo{Current: Version}, fmt.Errorf("update check: %s", response.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return UpdateInfo{Current: Version}, err
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	info := UpdateInfo{Current: Version, Latest: latest, URL: release.HTMLURL, Available: semverCompare(latest, Version) > 0}
	s.mu.Lock()
	s.updateChecked, s.updateInfo = time.Now(), info
	s.mu.Unlock()
	return info, nil
}

func semverCompare(a, b string) int {
	parse := func(value string) [3]int {
		value = strings.SplitN(value, "-", 2)[0]
		var out [3]int
		fmt.Sscanf(value, "%d.%d.%d", &out[0], &out[1], &out[2])
		return out
	}
	left, right := parse(a), parse(b)
	for n := range left {
		if left[n] < right[n] {
			return -1
		}
		if left[n] > right[n] {
			return 1
		}
	}
	return 0
}
