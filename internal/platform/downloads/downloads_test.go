package downloads

import "testing"

// CLI release targets (.github/workflows/build-cli.yml) without an upstream
// cloudflared build. They show no download button.
var unsupported = map[string]bool{"windows/arm64": true}

func TestManifestSanity(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range manifest {
		if err := a.Validate(); err != nil {
			t.Errorf("%s/%s: %v", a.GOOS, a.GOARCH, err)
		}
		key := a.Tool + " " + a.GOOS + "/" + a.GOARCH
		if seen[key] {
			t.Errorf("duplicate entry %s", key)
		}
		seen[key] = true
	}
	if len(manifest) == 0 {
		t.Skip("empty manifest: downloads are hidden everywhere")
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			_, ok := Lookup("cloudflared", goos, goarch)
			if ok == unsupported[goos+"/"+goarch] {
				t.Errorf("cloudflared %s/%s: entry %t, listed unsupported %t", goos, goarch, ok, unsupported[goos+"/"+goarch])
			}
		}
	}
}

func TestValidate(t *testing.T) {
	good := Artifact{Tool: "cloudflared", Version: "2026.1.0", GOOS: "linux", GOARCH: "amd64",
		URL: "https://example.com/x", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Size: 1, Format: FormatRaw}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Artifact){
		"http":       func(a *Artifact) { a.URL = "http://example.com/x" },
		"userinfo":   func(a *Artifact) { a.URL = "https://u:p@example.com/x" },
		"short hash": func(a *Artifact) { a.SHA256 = "abc" },
		"upper hash": func(a *Artifact) { a.SHA256 = "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef" },
		"size":       func(a *Artifact) { a.Size = 0 },
		"tool":       func(a *Artifact) { a.Tool = "ssh" },
		"version":    func(a *Artifact) { a.Version = "../1" },
		"tgz member": func(a *Artifact) { a.Format = FormatTGZ },
		"tgz path":   func(a *Artifact) { a.Format, a.Member = FormatTGZ, "../cloudflared" },
		"format":     func(a *Artifact) { a.Format = "zip" },
	} {
		a := good
		mutate(&a)
		if a.Validate() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, v := range []string{"", "1", "..", "1.2.3.4.5", "1.2-rc", "1.2/3", "1.2\x00"} {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q)", v)
		}
	}
}
