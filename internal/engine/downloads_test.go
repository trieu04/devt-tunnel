package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform/downloads"
)

const testVersion = "1.2.3"

func fakeToolBytes(version string) []byte {
	return []byte("#!/bin/sh\necho 'cloudflared version " + version + " (built today)'\n")
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

type tarEntry struct {
	name     string
	typeflag byte
	body     []byte
	link     string
}

func makeTGZ(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: 0o755, Size: int64(len(e.body)), Linkname: e.link}
		if e.typeflag != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type downloadFixture struct {
	e        *Engine
	server   *httptest.Server
	artifact downloads.Artifact
	data     string // DEVT_TUNNEL_DATA_DIR
	hits     atomic.Int64
	handler  http.HandlerFunc
	events   chan api.ToolDownloadProgress
}

func (f *downloadFixture) toolDir() string { return filepath.Join(f.data, "tools", "cloudflared") }
func (f *downloadFixture) exe() string {
	return filepath.Join(f.toolDir(), testVersion, "cloudflared")
}

// newDownloadFixture serves body at /asset over TLS and injects a manifest
// entry for it; mutate adjusts the entry (hash, size, format).
func newDownloadFixture(t *testing.T, cfg model.Config, body []byte, mutate func(*downloads.Artifact)) *downloadFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses shell-script stand-ins for cloudflared")
	}
	f := &downloadFixture{events: make(chan api.ToolDownloadProgress, 1024)}
	f.data = t.TempDir()
	t.Setenv("DEVT_TUNNEL_DATA_DIR", f.data)
	f.handler = func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "devt-tunnel/") {
			t.Errorf("User-Agent = %q", got)
		}
		f.handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	f.artifact = downloads.Artifact{Tool: "cloudflared", Version: testVersion, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		URL: f.server.URL + "/asset", SHA256: sum(body), Size: int64(len(body)), Format: downloads.FormatRaw}
	if mutate != nil {
		mutate(&f.artifact)
	}
	f.e, _, _ = newTestEngine(t, cfg)
	f.e.downloadHooks = downloadHooks{
		lookup: func(tool, goos, goarch string) (downloads.Artifact, bool) {
			return f.artifact, tool == "cloudflared" && goos == runtime.GOOS && goarch == runtime.GOARCH
		},
		transport: f.server.Client().Transport,
		progress:  time.Nanosecond,
	}
	cancel := f.e.Subscribe(func(event api.Event) {
		if event.Type == api.EventToolDownload && event.Download != nil {
			select {
			case f.events <- *event.Download:
			default:
			}
		}
	})
	t.Cleanup(cancel)
	return f
}

var person = api.Actor{Kind: "gui", Name: "gui"}

func errorKind(err error) api.ErrorKind {
	var rpc *api.Error
	if errors.As(err, &rpc) {
		return rpc.Data.Kind
	}
	return ""
}

// assertClean checks that nothing but (optionally) the version dir remains.
func (f *downloadFixture) assertNothingInstalled(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(f.toolDir())
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

func (f *downloadFixture) lastEvent(t *testing.T) api.ToolDownloadProgress {
	t.Helper()
	var last api.ToolDownloadProgress
	for {
		select {
		case ev := <-f.events:
			last = ev
		default:
			return last
		}
	}
}

func TestDownloadSuccessSelectsAndReuses(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), nil)
	tools, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.e.tools.Override("cloudflared"); got != f.exe() {
		t.Fatalf("override = %q, want %q", got, f.exe())
	}
	info, err := os.Stat(f.exe())
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("executable: %v %v", info, err)
	}
	if dir, _ := os.Stat(filepath.Join(f.toolDir(), testVersion)); dir.Mode().Perm() != 0o700 {
		t.Errorf("version dir mode = %v", dir.Mode().Perm())
	}
	for _, tool := range tools {
		if tool.Name == "cloudflared" && (tool.Path != f.exe() || tool.Origin != "downloaded" || tool.Download != nil) {
			t.Errorf("tool info = %+v", tool)
		}
	}
	if ev := f.lastEvent(t); ev.State != api.DownloadDone || ev.Received != f.artifact.Size || ev.Total != f.artifact.Size {
		t.Errorf("final event = %+v", ev)
	}
	// A verified version directory is reused without the network.
	if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", f.hits.Load())
	}
	// A tampered installed file is not trusted: it is replaced.
	if err := os.WriteFile(f.exe(), []byte("#!/bin/sh\necho evil\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(f.exe()); !bytes.Equal(got, fakeToolBytes(testVersion)) || f.hits.Load() != 2 {
		t.Errorf("tampered file not replaced: hits %d", f.hits.Load())
	}
}

func TestDownloadUseFalseKeepsOverride(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), nil)
	other := fakeCloudflared(t, filepath.Join(t.TempDir(), "bin"), "9.9.9", filepath.Join(t.TempDir(), "runs"))
	if _, err := f.e.SetToolPath("cloudflared", other); err != nil {
		t.Fatal(err)
	}
	tools, err := f.e.DownloadTool(context.Background(), person, "cloudflared", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.e.tools.Override("cloudflared"); got != other {
		t.Fatalf("override = %q, want %q", got, other)
	}
	if _, err := os.Stat(f.exe()); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name == "cloudflared" && (tool.Download == nil || tool.Download.Version != testVersion || tool.Download.Size != f.artifact.Size) {
			t.Errorf("selected 9.9.9 should still be offered %s: %+v", testVersion, tool.Download)
		}
	}
}

func TestDownloadFailuresLeaveNothing(t *testing.T) {
	body := fakeToolBytes(testVersion)
	for _, tc := range []struct {
		name    string
		mutate  func(*downloads.Artifact)
		handler func(w http.ResponseWriter, r *http.Request)
		kind    api.ErrorKind
		text    string
	}{
		{name: "checksum mismatch", mutate: func(a *downloads.Artifact) { a.SHA256 = strings.Repeat("0", 64) }, kind: api.ChecksumMismatch},
		{name: "oversize", mutate: func(a *downloads.Artifact) { a.Size--; a.SHA256 = sum(body[:len(body)-1]) }, kind: api.ChecksumMismatch},
		{name: "oversize unannounced", mutate: func(a *downloads.Artifact) { a.Size -= 5 }, kind: api.ChecksumMismatch,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.(http.Flusher).Flush()
				_, _ = w.Write(body)
			}},
		{name: "truncated", mutate: func(a *downloads.Artifact) { a.Size += 10 }, kind: api.Network, text: "connection closed"},
		{name: "http status", kind: api.Network, text: "404",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }},
		{name: "redirect to http", kind: api.Network, text: "non-HTTPS",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://"+r.Host+"/plain", http.StatusFound)
			}},
		{name: "version mismatch", mutate: func(a *downloads.Artifact) {}, kind: api.ChecksumMismatch, text: "does not report",
			handler: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			served := body
			if tc.name == "version mismatch" {
				served = fakeToolBytes("7.7.7")
			}
			f := newDownloadFixture(t, model.Config{}, served, tc.mutate)
			if tc.handler != nil {
				f.handler = tc.handler
			}
			_, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true)
			if errorKind(err) != tc.kind || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("err = %v (kind %q), want %s containing %q", err, errorKind(err), tc.kind, tc.text)
			}
			f.assertNothingInstalled(t)
			if f.e.tools.Override("cloudflared") != "" {
				t.Error("override set after a failed download")
			}
			if ev := f.lastEvent(t); ev.State != api.DownloadFailed || ev.Error == "" {
				t.Errorf("final event = %+v", ev)
			}
		})
	}
}

func TestDownloadRejectsNonHTTPSManifestURL(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), func(a *downloads.Artifact) {
		a.URL = strings.Replace(a.URL, "https://", "http://", 1)
	})
	if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); err == nil || f.hits.Load() != 0 {
		t.Fatalf("err = %v, hits = %d", err, f.hits.Load())
	}
}

func TestDownloadNotFoundWithoutArtifact(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), nil)
	if _, err := f.e.DownloadTool(context.Background(), person, "ngrok", true); errorKind(err) != api.NotFound {
		t.Fatalf("ngrok: %v", err)
	}
	if _, err := f.e.DownloadTool(context.Background(), person, "../x", true); errorKind(err) != api.Unknown {
		t.Fatalf("bad tool: %v", err)
	}
}

func TestDownloadCancelRemovesPartial(t *testing.T) {
	body := fakeToolBytes(testVersion)
	f := newDownloadFixture(t, model.Config{}, body, func(a *downloads.Artifact) { a.Size = 1 << 20 })
	sent := make(chan struct{})
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
		w.(http.Flusher).Flush()
		close(sent)
		<-r.Context().Done()
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true)
		done <- err
	}()
	<-sent
	partial := filepath.Join(f.toolDir(), "."+testVersion+".partial")
	waitFor(t, func() bool { _, err := os.Stat(partial); return err == nil })
	if err := f.e.DownloadCancel("cloudflared"); err != nil {
		t.Fatal(err)
	}
	err := receive(t, done)
	if errorKind(err) != api.Stopped {
		t.Fatalf("err = %v", err)
	}
	f.assertNothingInstalled(t)
	if ev := f.lastEvent(t); ev.State != api.DownloadCancelled {
		t.Errorf("final event = %+v", ev)
	}
	// Cancelling with nothing running is a no-op.
	if err := f.e.DownloadCancel("cloudflared"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadCancelledWhenAllWaitersLeave(t *testing.T) {
	body := fakeToolBytes(testVersion)
	f := newDownloadFixture(t, model.Config{}, body, func(a *downloads.Artifact) { a.Size = 1 << 20 })
	gone := make(chan struct{})
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(gone)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := f.e.DownloadTool(ctx, person, "cloudflared", true); done <- err }()
	waitFor(t, func() bool { return f.hits.Load() == 1 })
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	receive(t, gone)
}

func TestDownloadConcurrentRequestsJoin(t *testing.T) {
	body := fakeToolBytes(testVersion)
	f := newDownloadFixture(t, model.Config{}, body, nil)
	release := make(chan struct{})
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write(body)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); results <- err }()
	}
	waitFor(t, func() bool {
		f.e.downloadMu.Lock()
		defer f.e.downloadMu.Unlock()
		d := f.e.downloads["cloudflared"]
		return d != nil && d.waiters == 2
	})
	close(release)
	for range 2 {
		if err := receive(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if f.hits.Load() != 1 {
		t.Fatalf("hits = %d, want one shared download", f.hits.Load())
	}
}

func TestDownloadCleansStaleLeftovers(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), nil)
	if err := os.MkdirAll(filepath.Join(f.toolDir(), ".tmp-"+testVersion+"-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"." + testVersion + ".partial", ".0.9.partial"} {
		if err := os.WriteFile(filepath.Join(f.toolDir(), name), []byte("junk"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(f.toolDir(), "notes.txt") // not a name this code creates
	if err := os.WriteFile(keep, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", false); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(f.toolDir())
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != testVersion+",notes.txt" {
		t.Fatalf("entries = %v", names)
	}
}

func TestDownloadTGZ(t *testing.T) {
	member := fakeToolBytes(testVersion)
	reg := func(name string, body []byte) tarEntry {
		return tarEntry{name: name, typeflag: tar.TypeReg, body: body}
	}
	for _, tc := range []struct {
		name    string
		entries []tarEntry
		ok      bool
	}{
		{"plain", []tarEntry{reg("cloudflared", member)}, true},
		{"dot slash", []tarEntry{reg("./cloudflared", member)}, true},
		{"extra and traversal entries are skipped", []tarEntry{reg("README", []byte("x")), reg("../evil", []byte("x")), reg("../../evil", []byte("x")), reg("/tmp/evil", []byte("x")), reg("cloudflared", member)}, true},
		{"symlink member", []tarEntry{{name: "cloudflared", typeflag: tar.TypeSymlink, link: "/bin/sh"}}, false},
		{"hardlink member", []tarEntry{reg("other", member), {name: "cloudflared", typeflag: tar.TypeLink, link: "other"}}, false},
		{"directory member", []tarEntry{{name: "cloudflared", typeflag: tar.TypeDir}}, false},
		{"duplicate member", []tarEntry{reg("cloudflared", member), reg("cloudflared", member)}, false},
		{"only traversal names", []tarEntry{reg("../cloudflared", member), reg("x/../../cloudflared", member)}, false},
		{"missing member", []tarEntry{reg("README", []byte("x"))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := makeTGZ(t, tc.entries...)
			f := newDownloadFixture(t, model.Config{}, archive, func(a *downloads.Artifact) {
				a.Format, a.Member = downloads.FormatTGZ, "cloudflared"
			})
			_, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true)
			for _, p := range []string{filepath.Join(f.data, "evil"), filepath.Join(f.data, "tools", "evil"), filepath.Join(f.toolDir(), "evil")} {
				if _, statErr := os.Lstat(p); statErr == nil {
					t.Errorf("wrote %s", p)
				}
			}
			if !tc.ok {
				if errorKind(err) != api.ChecksumMismatch {
					t.Fatalf("err = %v", err)
				}
				f.assertNothingInstalled(t)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(filepath.Join(f.toolDir(), testVersion))
			if len(entries) != 2 || entries[0].Name() != installSidecar || entries[1].Name() != "cloudflared" {
				t.Fatalf("installed entries = %v", entries)
			}
			if got, _ := os.ReadFile(f.exe()); !bytes.Equal(got, member) {
				t.Fatal("member content differs")
			}
			// Reuse is verified through the recorded member hash.
			if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); err != nil || f.hits.Load() != 1 {
				t.Fatalf("reuse: %v, hits %d", err, f.hits.Load())
			}
		})
	}
}

func TestRemoveDownloaded(t *testing.T) {
	cfg := model.Config{Tunnels: []model.Tunnel{{ID: "cf", Name: "cf", Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000"}}}}
	f := newDownloadFixture(t, cfg, fakeToolBytes(testVersion), nil)
	if _, err := f.e.DownloadTool(context.Background(), person, "cloudflared", true); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"..", "../..", "1.2.3/../..", "/etc", "1.2.3\x00", "", "."} {
		if _, err := f.e.RemoveDownloadedTool(person, "cloudflared", version); errorKind(err) != api.Unknown {
			t.Errorf("version %q: %v", version, err)
		}
	}
	if _, err := f.e.RemoveDownloadedTool(person, "../tools", testVersion); errorKind(err) != api.Unknown {
		t.Errorf("tool traversal: %v", err)
	}
	if _, err := f.e.RemoveDownloadedTool(person, "cloudflared", "9.9.9"); errorKind(err) != api.NotFound {
		t.Errorf("absent version: %v", err)
	}

	if err := f.e.Start("cf"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, st := range f.e.Statuses() {
			if st.ID == "cf" && st.ToolPath == f.exe() && st.State != supervisor.StateStopped {
				return true
			}
		}
		return false
	})
	if _, err := f.e.RemoveDownloadedTool(person, "cloudflared", testVersion); errorKind(err) != api.Conflict {
		t.Fatalf("remove while running: %v", err)
	}
	if _, err := os.Stat(f.exe()); err != nil {
		t.Fatal("binary removed while in use")
	}
	f.e.Stop("cf")
	waitFor(t, func() bool {
		for _, st := range f.e.Statuses() {
			if st.ID == "cf" && st.State != supervisor.StateStopped && st.State != supervisor.StateFailed {
				return false
			}
		}
		return true
	})
	if _, err := f.e.RemoveDownloadedTool(person, "cloudflared", testVersion); err != nil {
		t.Fatal(err)
	}
	if f.e.tools.Override("cloudflared") != "" {
		t.Error("selected override not cleared")
	}
	if _, err := os.Stat(filepath.Join(f.toolDir(), testVersion)); !os.IsNotExist(err) {
		t.Errorf("version dir remains: %v", err)
	}
}

func TestRemoveDownloadedRejectsSymlinkedVersion(t *testing.T) {
	f := newDownloadFixture(t, model.Config{}, fakeToolBytes(testVersion), nil)
	outside := t.TempDir()
	victim := filepath.Join(outside, "keep")
	if err := os.WriteFile(victim, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.toolDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.toolDir(), "4.5.6")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.RemoveDownloadedTool(person, "cloudflared", "4.5.6"); err == nil {
		t.Fatal("symlinked version accepted")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("file outside the tools directory was deleted")
	}
}

func TestStartNotFoundSuggestsDownload(t *testing.T) {
	cfg := model.Config{Tunnels: []model.Tunnel{{ID: "cf", Name: "cf", Type: model.TypeCloudflared, Cloudflared: &model.Cloudflared{URL: "http://localhost:3000"}}}}
	f := newDownloadFixture(t, cfg, fakeToolBytes(testVersion), nil)
	// The machine may have its own cloudflared; a stale override is missing either way.
	f.e.tools.SetOverrides(map[string]string{"cloudflared": filepath.Join(t.TempDir(), "cloudflared")})
	err := f.e.Start("cf")
	if errorKind(err) != api.NotFound || !strings.Contains(err.Error(), "devt-tunnel tools download cloudflared") {
		t.Fatalf("err = %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
