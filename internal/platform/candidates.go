package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Candidate origins.
const (
	OriginPath       = "path"
	OriginHomebrew   = "homebrew"
	OriginScoop      = "scoop"
	OriginWinGet     = "winget"
	OriginSystem     = "system"
	OriginDownloaded = "downloaded"
	OriginCustom     = "custom"
)

// Candidate is one executable that could serve a tool, before version probing.
type Candidate struct {
	Path     string
	Resolved string
	Origin   string
}

// ToolOrigin classifies an executable by where it lives. toolsDir is the managed
// directory ("" when unknown). It is pure so every OS can be tested anywhere.
func ToolOrigin(goos, path, toolsDir string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
		path = strings.ReplaceAll(path, "/", `\`)
		toolsDir = strings.ReplaceAll(toolsDir, "/", `\`)
	}
	norm := func(s string) string {
		if goos == "windows" {
			return strings.ToLower(s)
		}
		return s
	}
	p := norm(path)
	if toolsDir != "" {
		if d := norm(strings.TrimRight(toolsDir, sep)) + sep; strings.HasPrefix(p, d) {
			return OriginDownloaded
		}
	}
	if goos == "windows" {
		switch {
		case strings.Contains(p, `\scoop\shims\`) || strings.Contains(p, `\scoop\apps\`):
			return OriginScoop
		case strings.Contains(p, `\winget\links\`) || strings.Contains(p, `\winget\packages\`):
			return OriginWinGet
		case strings.Contains(p, `\system32\openssh\`):
			return OriginSystem
		}
		return OriginPath
	}
	switch {
	case strings.HasPrefix(p, "/opt/homebrew/"), strings.HasPrefix(p, "/usr/local/Cellar/"),
		strings.HasPrefix(p, "/home/linuxbrew/.linuxbrew/"):
		return OriginHomebrew
	case strings.HasPrefix(p, "/usr/bin/"), strings.HasPrefix(p, "/bin/"):
		return OriginSystem
	}
	return OriginPath
}

// MergeCandidates drops entries whose Resolved paths repeat, keeping the first.
func MergeCandidates(in []Candidate) []Candidate {
	seen := map[string]bool{}
	out := make([]Candidate, 0, len(in))
	for _, c := range in {
		key := c.Resolved
		if key == "" {
			key = c.Path
		}
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func exeName(tool string) string {
	if runtime.GOOS == "windows" && filepath.Ext(tool) == "" {
		return tool + ".exe"
	}
	return tool
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func resolved(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

// Candidates lists every executable for tool: all PATH directories, then the
// managed tools directory, then the current override when it is elsewhere.
func (t *Tools) Candidates(tool string) []Candidate {
	toolsDir, _ := ToolsDir()
	return discover(tool, filepath.SplitList(t.PATH()), toolsDir, t.Override(tool))
}

func discover(tool string, dirs []string, toolsDir, override string) []Candidate {
	var found []Candidate
	add := func(path, origin string) {
		if !isExecutable(path) {
			return
		}
		r := resolved(path)
		if origin == OriginPath { // /usr/local/bin/x -> .../Cellar/... is still Homebrew
			if ToolOrigin(runtime.GOOS, r, toolsDir) == OriginHomebrew {
				origin = OriginHomebrew
			}
		}
		found = append(found, Candidate{Path: path, Resolved: r, Origin: origin})
	}
	exe := exeName(tool)
	for _, dir := range dirs {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, exe)
		add(p, ToolOrigin(runtime.GOOS, p, toolsDir))
	}
	if toolsDir != "" {
		entries, _ := os.ReadDir(filepath.Join(toolsDir, tool))
		for i := len(entries) - 1; i >= 0; i-- { // newest-looking version name first
			if entries[i].IsDir() && !strings.HasPrefix(entries[i].Name(), ".") {
				add(filepath.Join(toolsDir, tool, entries[i].Name(), exe), OriginDownloaded)
			}
		}
	}
	out := MergeCandidates(found)
	if override != "" {
		r := resolved(override)
		known := false
		for _, c := range out {
			if c.Path == override || c.Resolved == r {
				known = true
			}
		}
		if !known {
			out = append(out, Candidate{Path: override, Resolved: r, Origin: OriginCustom})
		}
	}
	return out
}
