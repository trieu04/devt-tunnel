package engine

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/command"
	"github.com/trieu04/devt-tunnel/internal/core/config"
	"github.com/trieu04/devt-tunnel/internal/core/importer"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/state"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

func (s *Engine) EvaluateDraft(t model.Tunnel) api.DraftResult {
	preview, err := s.manager.PreviewDraft(t)
	result := api.DraftResult{Preview: preview}
	if err != nil {
		result.BuildError = err.Error()
	}
	cfg := s.manager.Config()
	result.Findings = checks.Run(t, cfg, s.checkDeps(t))
	if result.Findings == nil {
		result.Findings = []checks.Finding{}
	}
	return result
}

func (s *Engine) checkDeps(t model.Tunnel) checks.Deps {
	own := map[string]bool{}
	for _, st := range s.manager.Statuses() {
		if st.ID != t.ID || (st.State != supervisor.StateStarting && st.State != supervisor.StateRunning && st.State != supervisor.StateReconnecting) {
			continue
		}
		for _, saved := range s.manager.Config().Tunnels {
			if saved.ID == t.ID {
				for _, f := range saved.Forwards {
					if f.Kind != model.ForwardRemote {
						own[command.DialAddress(f.Bind)] = true
					}
				}
			}
		}
	}
	profileID := firstProfile(t)
	return checks.Deps{
		Resolve: s.tools.Resolve, Secrets: s.secrets, Shell: s.shell, Home: platform.Home(), Dialect: platform.Dialect(), ProfileID: profileID,
		Listening: func(address string) bool {
			if own[address] {
				return false
			}
			host, port, err := net.SplitHostPort(address)
			if err != nil || strings.Contains(port, "/") {
				return false
			}
			if host == "localhost" {
				host = "127.0.0.1"
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return false
			}
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 100*time.Millisecond)
			if err != nil {
				return false
			}
			conn.Close()
			return true
		},
		Hosts: func(id string) ([]string, error) {
			list := s.SSHHosts(id)
			hosts := make([]string, 0, len(list.Hosts))
			for _, host := range list.Hosts {
				hosts = append(hosts, host.Alias)
			}
			return hosts, nil
		},
	}
}

func firstProfile(t model.Tunnel) string {
	if len(t.Profiles) > 0 {
		return t.Profiles[0]
	}
	return ""
}

func (s *Engine) NewDraft(kind model.TunnelType) model.Tunnel {
	t := model.Tunnel{Type: kind, Reconnect: true}
	switch kind {
	case model.TypeSSH:
		t.SSH = &model.SSH{Flags: []string{"-N"}, Options: map[string]string{"ServerAliveInterval": "30"}}
	case model.TypeNgrok:
		t.Ngrok = &model.Ngrok{Proto: "http", Addr: "http://127.0.0.1:3000"}
	case model.TypeCloudflared:
		t.Cloudflared = &model.Cloudflared{URL: "http://127.0.0.1:3000"}
	case model.TypeCommand:
		t.Command = &model.Command{}
	}
	return t
}

func (s *Engine) ImportCommand(line string) (importer.Result, error) {
	profileID := s.stateStore.Get().ActiveProfile
	hostList := s.SSHHosts(profileID)
	hosts := make([]string, 0, len(hostList.Hosts))
	for _, host := range hostList.Hosts {
		hosts = append(hosts, host.Alias)
	}
	return importer.Parse(line, platform.Dialect(), hosts)
}

func (s *Engine) DuplicateDraft(id string) (model.Tunnel, error) {
	cfg := s.manager.Config()
	taken := map[string]bool{}
	for _, tunnel := range cfg.Tunnels {
		taken[tunnel.ID] = true
		if tunnel.ID == id {
			copy := tunnel
			copy.Name += " copy"
			copy.ID = model.NewID(copy.Name, taken)
			return copy, nil
		}
	}
	return model.Tunnel{}, fmt.Errorf("unknown tunnel %q", id)
}

func (s *Engine) SaveTunnel(t model.Tunnel, start bool) (string, error) {
	wasNew := t.ID == ""
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		if wasNew {
			taken := map[string]bool{}
			for _, candidate := range cfg.Tunnels {
				taken[candidate.ID] = true
			}
			t.ID = model.NewID(t.Name, taken)
			cfg.Tunnels = append(cfg.Tunnels, t)
			return nil
		}
		for n := range cfg.Tunnels {
			if cfg.Tunnels[n].ID == t.ID {
				cfg.Tunnels[n] = t
				return nil
			}
		}
		return fmt.Errorf("unknown tunnel %q", t.ID)
	})
	if err != nil {
		if errors.Is(err, config.ErrConflict) {
			return "", fmt.Errorf("tunnels.toml changed on disk: %w", err)
		}
		return "", err
	}
	s.manager.SetConfig(cfg)
	if start {
		if err := s.manager.Restart(t.ID); err != nil {
			return t.ID, err
		}
		s.recordUsed(t.ID)
	}
	return t.ID, nil
}

func (s *Engine) DeleteTunnel(id string) error {
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		out := cfg.Tunnels[:0]
		found := false
		for _, tunnel := range cfg.Tunnels {
			if tunnel.ID == id {
				found = true
				continue
			}
			out = append(out, tunnel)
		}
		if !found {
			return fmt.Errorf("unknown tunnel %q", id)
		}
		cfg.Tunnels = out
		model.RemoveTunnelReferences(cfg, id)
		return nil
	})
	if err != nil {
		return err
	}
	s.manager.SetConfig(cfg)
	return s.stateStore.Update(func(st *state.State) {
		delete(st.LastUsed, id)
	})
}

func (s *Engine) SetFavorite(id string, favorite bool) error {
	cfg, err := s.configStore.Update(func(cfg *model.Config) error {
		for n := range cfg.Tunnels {
			if cfg.Tunnels[n].ID == id {
				cfg.Tunnels[n].Favorite = favorite
				return nil
			}
		}
		return fmt.Errorf("unknown tunnel %q", id)
	})
	if err == nil {
		s.manager.SetConfig(cfg)
	}
	return err
}

func (s *Engine) Restart(id string) error { return s.manager.Restart(id) }

func (s *Engine) recordUsed(id string) {
	_ = s.stateStore.Update(func(st *state.State) {
		if st.LastUsed == nil {
			st.LastUsed = map[string]time.Time{}
		}
		st.LastUsed[id] = time.Now()
	})
}

func removeString(values []string, value string) []string {
	out := values[:0]
	for _, candidate := range values {
		if candidate != value {
			out = append(out, candidate)
		}
	}
	return out
}
