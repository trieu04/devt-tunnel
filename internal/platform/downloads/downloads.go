// Package downloads lists the tool releases devt-tunnel may fetch on a person's
// explicit request. The list is generated (manifest_gen.go, `task
// downloads:manifest`) and compiled in: no caller can supply a URL or hash.
package downloads

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
)

//go:generate go run ./gen -o manifest_gen.go

// Artifact is one pinned release file for a tool and platform.
type Artifact struct {
	Tool, Version string
	GOOS, GOARCH  string
	URL           string // https only
	SHA256        string // lowercase hex of the downloaded file
	Size          int64  // exact; a download aborts past it
	Format        string // raw | tgz
	Member        string // file inside the archive, for tgz
}

const (
	FormatRaw = "raw"
	FormatTGZ = "tgz"
)

// Tools that may ever be downloaded.
var Tools = []string{"cloudflared"}

// Manifest is the compiled-in list. Tests may replace it; production code reads
// it only through Lookup.
var Manifest = manifest

var (
	versionPattern = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,9}){1,3}$`)
	hashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	memberPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// ValidVersion reports whether v is a strict dotted-numeric release version,
// safe to use as a single path element.
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

// Lookup returns the artifact for tool on goos/goarch.
func Lookup(tool, goos, goarch string) (Artifact, bool) {
	return lookup(Manifest, tool, goos, goarch)
}

func lookup(list []Artifact, tool, goos, goarch string) (Artifact, bool) {
	for _, a := range list {
		if a.Tool == tool && a.GOOS == goos && a.GOARCH == goarch {
			return a, true
		}
	}
	return Artifact{}, false
}

// Validate checks an artifact's shape. The downloader refuses an invalid one.
func (a Artifact) Validate() error {
	known := false
	for _, t := range Tools {
		known = known || t == a.Tool
	}
	switch {
	case !known:
		return fmt.Errorf("artifact: unsupported tool %q", a.Tool)
	case !ValidVersion(a.Version):
		return errors.New("artifact: invalid version")
	case a.GOOS == "" || a.GOARCH == "":
		return errors.New("artifact: missing platform")
	case !hashPattern.MatchString(a.SHA256):
		return errors.New("artifact: SHA-256 must be 64 lowercase hex characters")
	case a.Size <= 0:
		return errors.New("artifact: size must be positive")
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("artifact: URL must be https")
	}
	switch a.Format {
	case FormatRaw:
		if a.Member != "" {
			return errors.New("artifact: raw artifacts have no member")
		}
	case FormatTGZ:
		if !memberPattern.MatchString(a.Member) {
			return errors.New("artifact: tgz artifacts must name a plain member")
		}
	default:
		return fmt.Errorf("artifact: unknown format %q", a.Format)
	}
	return nil
}
