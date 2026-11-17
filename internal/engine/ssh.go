package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/command"
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

var toolVersionArgs = map[string][]string{"ssh": {"-V"}, "ngrok": {"version"}, "cloudflared": {"--version"}}

var toolNames = []string{"ssh", "ngrok", "cloudflared"}

func (s *Engine) Tools() []api.ToolInfo {
	out := make([]api.ToolInfo, 0, len(toolNames))
	toolsDir, _ := platform.ToolsDir()
	for _, name := range toolNames {
		info := api.ToolInfo{Name: name}
		path, source, err := s.tools.Locate(name)
		if err != nil {
			info.Error = err.Error()
			info.Download = s.downloadOffer(name, "")
			out = append(out, info)
			continue
		}
		info.Path, info.Source = path, source
		info.Origin = platform.ToolOrigin(runtime.GOOS, path, toolsDir)
		if info.Origin == platform.OriginPath && source == "settings" {
			info.Origin = platform.OriginCustom
		}
		version, err := s.probeVersion(path, name)
		info.Version = version
		if err != nil {
			info.Error = err.Error()
		}
		info.Download = s.downloadOffer(name, version)
		out = append(out, info)
	}
	return out
}

type probeKey struct {
	path  string
	size  int64
	mtime int64
}

// probeVersion runs the tool's version command, caching successes by
// (resolved path, size, mtime) so repeated page opens do not re-run binaries.
func (s *Engine) probeVersion(path, tool string) (string, error) {
	var key probeKey
	if info, err := os.Stat(path); err == nil {
		r := path
		if e, err := filepath.EvalSymlinks(path); err == nil {
			r = e
		}
		key = probeKey{r, info.Size(), info.ModTime().UnixNano()}
		if v, ok := s.probeCache.Load(key); ok {
			return v.(string), nil
		}
	}
	out, err := platform.RunTool(context.Background(), path, toolVersionArgs[tool]...)
	text := strings.TrimSpace(string(out))
	if err == nil && key.path != "" {
		s.probeCache.Store(key, text)
	}
	return text, err
}

// ToolCandidates lists every executable that could serve a tool.
func (s *Engine) ToolCandidates(tool string) ([]api.ToolCandidate, error) {
	if _, ok := toolVersionArgs[tool]; !ok {
		return nil, fmt.Errorf("unsupported tool %q", tool)
	}
	found := s.tools.Candidates(tool)
	override := s.tools.Override(tool)
	autoResolved := ""
	if auto, err := s.tools.Auto(tool); err == nil {
		autoResolved = auto
		if r, err := filepath.EvalSymlinks(auto); err == nil {
			autoResolved = r
		}
	}
	out := make([]api.ToolCandidate, len(found))
	var wg sync.WaitGroup
	for i, c := range found {
		out[i] = api.ToolCandidate{Path: c.Path, Resolved: c.Resolved, Origin: c.Origin,
			Auto: autoResolved != "" && c.Resolved == autoResolved, Selected: override != "" && c.Path == override}
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.probeVersion(c.Path, tool)
			out[i].Version = firstLine(v)
			if err != nil {
				out[i].Version, out[i].Error = "", err.Error()
			}
		}()
	}
	wg.Wait()
	return out, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// SetToolPath changes the override as the local GUI; see SetToolPathFor.
func (s *Engine) SetToolPath(tool, path string) ([]api.ToolInfo, error) {
	return s.SetToolPathFor(api.Actor{Kind: "gui", Name: "gui"}, tool, path)
}

func (s *Engine) SetToolPathFor(actor api.Actor, tool, path string) ([]api.ToolInfo, error) {
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
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.ToolSetPath, Tool: tool, ToolVersion: managedVersion(tool, path), Result: audit.ResultOK}); err != nil {
		return nil, err
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

// Safe SSH definitions put a profile's -F first; AgentSafe rejects agent-
// supplied -F options. Inspect the built argv so active-profile selection and
// fallback through multiple memberships exactly match actual execution.
func (s *Engine) builtAliasExists(alias string, spec command.Spec) bool {
	path := filepath.Join(platform.Home(), ".ssh", "config")
	if len(spec.Args) >= 2 && spec.Args[0] == "-F" {
		path = spec.Args[1]
	}
	hosts, _, err := sshconfig.Scan(path, osFS{})
	if err != nil {
		return false
	}
	for _, host := range hosts {
		if host.Alias == alias {
			return true
		}
	}
	return false
}
