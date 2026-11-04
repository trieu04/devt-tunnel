package sshconfig

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type memFS map[string]string

func (m memFS) ReadFile(name string) ([]byte, error) {
	v, ok := m[filepath.Clean(name)]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(v), nil
}

func (m memFS) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m {
		ok, err := filepath.Match(filepath.Clean(pattern), filepath.Clean(name))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, name)
		}
	}
	// filepath.Glob returns matches in lexical order.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func TestScanRules(t *testing.T) {
	const home = "/home/tester"
	main := filepath.Join(home, ".ssh", "config")
	fs := memFS{
		main: "# comment\r\n hOsT = \"prod db\" staging *.internal !hidden ?box\r\nInclude includes/*.conf\r\nInclude absent.conf\r\nMatch host ignored\r\n Host inside-match\r\nHost duplicate\r\n",
		filepath.Join(home, ".ssh", "includes", "a.conf"): "Host included\nInclude cycle.conf\nHost duplicate other\n",
		filepath.Join(home, ".ssh", "cycle.conf"):         "Host cyclic\nInclude config\n",
	}
	hosts, warnings, err := Scan(main, fs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Host{
		{Alias: "prod db", Source: main},
		{Alias: "staging", Source: main},
		{Alias: "included", Source: filepath.Join(home, ".ssh", "includes", "a.conf")},
		{Alias: "cyclic", Source: filepath.Join(home, ".ssh", "cycle.conf")},
		{Alias: "duplicate", Source: filepath.Join(home, ".ssh", "includes", "a.conf")},
		{Alias: "other", Source: filepath.Join(home, ".ssh", "includes", "a.conf")},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "absent.conf") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestScanIncludeTildeAndDepth(t *testing.T) {
	const home = "/home/tester"
	main := filepath.Join(home, ".ssh", "config")
	fs := memFS{main: "Include ~/parts/*.conf\n", filepath.Join(home, "parts", "one.conf"): "Host tilde\nInclude " + strings.Repeat("../", 1) + ".ssh/deep0\n"}
	for i := 0; i <= maxIncludeDepth; i++ {
		name := filepath.Join(home, ".ssh", "deep"+itoa(i))
		if i == maxIncludeDepth {
			fs[name] = "Host deepest"
		} else {
			fs[name] = "Include deep" + itoa(i+1) + "\nHost depth" + itoa(i)
		}
	}
	hosts, warnings, err := Scan(main, fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) == 0 || hosts[0].Alias != "tilde" {
		t.Fatalf("tilde include not read: %#v", hosts)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[len(warnings)-1], "depth limit") {
		t.Fatalf("depth warning missing: %#v", warnings)
	}
}

func TestScanRelativeIncludeAndFirstDefinition(t *testing.T) {
	const dir = "/home/u/.ssh"
	main := filepath.Join(dir, "config")
	fs := memFS{main: "Host x\nInclude child.conf\n", filepath.Join(dir, "child.conf"): "Host x y\n"}
	hosts, _, err := Scan(main, fs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Host{{Alias: "x", Source: main}, {Alias: "y", Source: filepath.Join(dir, "child.conf")}}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
}

func TestScanMissingRootIsError(t *testing.T) {
	if _, _, err := Scan("/missing/config", memFS{}); err == nil {
		t.Fatal("expected root read error")
	}
}

func TestParseG(t *testing.T) {
	got := ParseG([]byte("hostname example.com\r\nuser alice\r\nport 2200\r\nproxyjump bastion\r\nidentityfile ~/.ssh/key one\r\nidentityfile \"/keys/second key\"\r\nunknown ignored\n"))
	want := Resolved{HostName: "example.com", User: "alice", Port: "2200", ProxyJump: "bastion", IdentityFiles: []string{"~/.ssh/key one", "/keys/second key"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseG = %#v, want %#v", got, want)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
