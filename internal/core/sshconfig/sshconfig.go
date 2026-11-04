// Package sshconfig scans OpenSSH configuration files for host aliases and
// parses resolved values emitted by ssh -G.
package sshconfig

import (
	"bufio"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// Host is an alias declared in an ssh configuration file.
type Host struct {
	Alias  string `json:"alias"`
	Source string `json:"source"`
}

// FS is the filesystem surface needed by Scan.
type FS interface {
	ReadFile(name string) ([]byte, error)
	Glob(pattern string) ([]string, error)
}

const maxIncludeDepth = 16

// Scan returns unique literal host aliases in first-definition order. Missing
// included files and unreadable includes are warnings; failure to read path is
// returned as an error.
func Scan(configPath string, fsys FS) ([]Host, []string, error) {
	if fsys == nil {
		return nil, nil, io.ErrUnexpectedEOF
	}
	root, home, sshDir := cleanPath(configPath), homeDir(configPath), filepath.Dir(configPath)
	s := scanner{fs: fsys, root: root, home: home, sshDir: cleanPath(sshDir), seen: map[string]bool{}, visited: map[string]bool{}}
	if err := s.file(root, 0); err != nil {
		return nil, s.warnings, err
	}
	return s.hosts, s.warnings, nil
}

type scanner struct {
	fs       FS
	root     string
	home     string
	sshDir   string
	hosts    []Host
	warnings []string
	seen     map[string]bool
	visited  map[string]bool
}

func (s *scanner) file(name string, depth int) error {
	name = cleanPath(name)
	if depth > maxIncludeDepth {
		s.warnings = append(s.warnings, "Include depth limit reached at "+name)
		return nil
	}
	if s.visited[name] {
		return nil
	}
	s.visited[name] = true
	b, err := s.fs.ReadFile(name)
	if err != nil {
		if name == s.root {
			return err
		}
		s.warnings = append(s.warnings, "cannot read Include "+name+": "+err.Error())
		return nil
	}
	matchBlock := false
	scan := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(b), "\r\n", "\n")))
	for scan.Scan() {
		key, values, ok := directive(scan.Text())
		if !ok {
			continue
		}
		switch key {
		case "host":
			if matchBlock {
				continue
			}
			for _, v := range values {
				if strings.ContainsAny(v, "*?!") || v == "" || s.seen[v] {
					continue
				}
				s.seen[v] = true
				s.hosts = append(s.hosts, Host{Alias: v, Source: name})
			}
		case "match":
			matchBlock = true
		case "include":
			for _, pattern := range values {
				for _, target := range s.expand(pattern) {
					if err := s.file(target, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	return nil
}

func (s *scanner) expand(pattern string) []string {
	pattern = strings.TrimSpace(pattern)
	if strings.HasPrefix(pattern, "~") {
		if pattern == "~" {
			pattern = s.home
		} else if strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, `~\`) {
			pattern = filepath.Join(s.home, pattern[2:])
		}
	} else if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(s.sshDir, pattern)
	}
	pattern = cleanPath(pattern)
	matches, err := s.fs.Glob(pattern)
	if err != nil {
		s.warnings = append(s.warnings, "invalid Include pattern "+pattern+": "+err.Error())
		return nil
	}
	if len(matches) == 0 {
		s.warnings = append(s.warnings, "Include matched no files: "+pattern)
	}
	for i := range matches {
		matches[i] = cleanPath(matches[i])
	}
	return matches
}

func directive(line string) (string, []string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil, false
	}
	fields, ok := splitFields(line)
	if !ok || len(fields) < 2 {
		return "", nil, false
	}
	key := strings.ToLower(strings.TrimSuffix(fields[0], "="))
	values := fields[1:]
	if len(values) > 0 && values[0] == "=" {
		values = values[1:]
	}
	if len(values) == 0 {
		return "", nil, false
	}
	return key, values, true
}

// splitFields handles quotes and the common Keyword=value / Keyword = value
// spellings without interpreting unrelated SSH configuration semantics.
func splitFields(line string) ([]string, bool) {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped, active := false, false
	flush := func() {
		if active {
			fields = append(fields, b.String())
			b.Reset()
			active = false
		}
	}
	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped, active = false, true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			active = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			active = true
			continue
		}
		if r == '"' || r == '\'' {
			quote, active = r, true
			continue
		}
		if r == '#' && !active {
			break
		}
		if r == '=' || r == ' ' || r == '\t' {
			flush()
			if r == '=' {
				fields = append(fields, "=")
			}
			continue
		}
		b.WriteRune(r)
		active = true
	}
	if escaped || quote != 0 {
		return nil, false
	}
	flush()
	return fields, len(fields) > 0
}

func homeDir(configPath string) string {
	// The default path is ~/.ssh/config; injected filesystems can mirror it.
	return cleanPath(filepath.Dir(filepath.Dir(configPath)))
}

func cleanPath(p string) string {
	if filepath.Separator == '\\' {
		return filepath.Clean(p)
	}
	return path.Clean(p)
}

// Resolved contains values parsed from ssh -G output.
type Resolved struct {
	HostName, User, Port, ProxyJump string
	IdentityFiles                   []string
}

// ParseG extracts the resolved values relevant to tunnel setup from ssh -G.
func ParseG(output []byte) Resolved {
	var r Resolved
	scan := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(output), "\r\n", "\n")))
	for scan.Scan() {
		fields, ok := splitFields(strings.TrimSpace(scan.Text()))
		if !ok || len(fields) < 2 {
			continue
		}
		value := strings.Join(fields[1:], " ")
		switch strings.ToLower(fields[0]) {
		case "hostname":
			r.HostName = value
		case "user":
			r.User = value
		case "port":
			r.Port = value
		case "proxyjump":
			r.ProxyJump = value
		case "identityfile":
			r.IdentityFiles = append(r.IdentityFiles, value)
		}
	}
	return r
}
