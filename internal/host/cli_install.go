package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CLIStatus describes whether the `devt-tunnel` binary bundled with the app is
// reachable from a terminal. Linux packages and the Windows installer put it
// on PATH themselves, so only macOS needs the in-app install.
type CLIStatus struct {
	// Supported is true when the app can link the bundled CLI itself.
	Supported bool `json:"supported"`
	// Bundled is the path of the devt-tunnel binary shipped next to the app executable.
	Bundled string `json:"bundled"`
	// Link is where the CLI is (or would be) linked.
	Link string `json:"link"`
	// Installed is true when Link already points at the bundled binary.
	Installed bool `json:"installed"`
	// OnPath is true when Link's directory is on the current PATH.
	OnPath bool `json:"onPath"`
}

func bundledCLI() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	name := "devt-tunnel"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(exe), name)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", errors.New("bundled devt-tunnel not found next to the app")
	}
	return path, nil
}

func dirOnPath(dir string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(entry) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// cliLinkCandidates lists link locations in order of preference: a system
// directory that needs no prompt when writable, then a per-user one.
func cliLinkCandidates() []string {
	candidates := []string{"/usr/local/bin/devt-tunnel"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "devt-tunnel"))
	}
	return candidates
}

func linkTarget(link string) string {
	target, err := os.Readlink(link)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return target
}

func (s *Service) CLIStatus() CLIStatus {
	status := CLIStatus{Supported: runtime.GOOS == "darwin"}
	bundled, err := bundledCLI()
	if err != nil {
		status.Supported = false
		return status
	}
	status.Bundled = bundled
	candidates := cliLinkCandidates()
	status.Link = candidates[0]
	for _, link := range candidates {
		if linkTarget(link) == bundled {
			status.Link, status.Installed, status.OnPath = link, true, dirOnPath(filepath.Dir(link))
			return status
		}
	}
	return status
}

// InstallCLI links the bundled devt-tunnel binary into a directory on the user's PATH
// and returns the link path. It never overwrites a file it did not create.
func (s *Service) InstallCLI() (CLIStatus, error) {
	status := s.CLIStatus()
	if !status.Supported {
		return status, errors.New("the CLI is installed with the app on this platform")
	}
	if status.Installed {
		return status, nil
	}
	var failures []string
	for _, link := range cliLinkCandidates() {
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if _, err := os.Lstat(link); err == nil {
			if target := linkTarget(link); target == "" || !strings.HasSuffix(target, "/devt-tunnel") {
				failures = append(failures, link+" already exists and is not a link to devt-tunnel")
				continue
			}
			if err := os.Remove(link); err != nil {
				failures = append(failures, err.Error())
				continue
			}
		}
		if err := os.Symlink(status.Bundled, link); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		return s.CLIStatus(), nil
	}
	return status, fmt.Errorf("could not install devt-tunnel: %s", strings.Join(failures, "; "))
}
