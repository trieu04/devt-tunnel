package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/settings"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (osFS) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

func (s *Engine) SSHHosts(profileID string) api.SSHHostList {
	path := s.sshConfigPath(profileID)
	hosts, warnings, err := sshconfig.Scan(path, osFS{})
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	return api.SSHHostList{ConfigPath: path, Hosts: hosts, Warnings: warnings}
}

func (s *Engine) sshConfigPath(profileID string) string {
	for _, profile := range s.manager.Config().Profiles {
		if profile.ID == profileID && profile.SSHConfig != "" {
			return expandPath(profile.SSHConfig)
		}
	}
	return filepath.Join(platform.Home(), ".ssh", "config")
}

func expandPath(path string) string {
	if path == "~" {
		return platform.Home()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(platform.Home(), path[2:])
	}
	return path
}

func (s *Engine) ResolveSSHHost(alias, profileID string) (sshconfig.Resolved, error) {
	path, err := s.tools.Resolve("ssh")
	if err != nil {
		return sshconfig.Resolved{}, err
	}
	args := []string{"-G"}
	if configPath := s.sshConfigPath(profileID); configPath != filepath.Join(platform.Home(), ".ssh", "config") {
		args = append(args, "-F", configPath)
	}
	args = append(args, alias)
	out, err := platform.RunTool(context.Background(), path, args...)
	if err != nil {
		return sshconfig.Resolved{}, fmt.Errorf("ssh -G: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return sshconfig.ParseG(out), nil
}

func (s *Engine) ReloadSSHHosts() { s.notify() }

func (s *Engine) Tools() []api.ToolInfo {
	commands := map[string][]string{"ssh": {"-V"}, "ngrok": {"version"}, "cloudflared": {"--version"}}
	out := make([]api.ToolInfo, 0, len(commands))
	for _, name := range []string{"ssh", "ngrok", "cloudflared"} {
		info := api.ToolInfo{Name: name}
		path, source, err := s.tools.Locate(name)
		if err != nil {
			info.Error = err.Error()
			out = append(out, info)
			continue
		}
		info.Path, info.Source = path, source
		version, err := platform.RunTool(context.Background(), path, commands[name]...)
		info.Version = strings.TrimSpace(string(version))
		if err != nil {
			info.Error = err.Error()
		}
		out = append(out, info)
	}
	return out
}

func (s *Engine) SetToolPath(tool, path string) ([]api.ToolInfo, error) {
	if tool != "ssh" && tool != "ngrok" && tool != "cloudflared" {
		return nil, fmt.Errorf("unsupported tool %q", tool)
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return nil, errors.New("tool path must be absolute")
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
			return nil, errors.New("tool path must be an executable file")
		}
	}
	if err := s.updateSettings(func(next *settings.Settings) {
		if next.Tools == nil {
			next.Tools = map[string]string{}
		}
		if path == "" {
			delete(next.Tools, tool)
		} else {
			next.Tools[tool] = path
		}
	}); err != nil {
		return nil, err
	}
	if err := s.lifetimeError(); err != nil {
		return nil, err
	}
	s.notify()
	return s.Tools(), nil
}
