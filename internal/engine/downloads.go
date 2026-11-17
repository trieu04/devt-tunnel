package engine

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/audit"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
	"github.com/trieu04/devt-tunnel/internal/platform"
	"github.com/trieu04/devt-tunnel/internal/platform/downloads"
)

// Downloads run in the daemon, only on a person's explicit tool.download call,
// and only fetch artifacts compiled into downloads.Manifest. The SHA-256 check
// is the only verification: Go-written files carry no quarantine attribute or
// Mark of the Web, so Gatekeeper and SmartScreen never inspect them.

const (
	downloadConnectTimeout = 30 * time.Second
	// A body that delivers nothing for this long is treated as a dead connection;
	// there is deliberately no overall deadline for slow links.
	downloadStallTimeout = 60 * time.Second
	downloadProgressGap  = 250 * time.Millisecond
	// Bounds for tgz extraction: the member itself, and everything gunzipped.
	maxExtractedMember = 512 << 20
	maxDecompressed    = 1 << 30
	installSidecar     = ".devt-sha256"
)

type downloadHooks struct {
	lookup    func(tool, goos, goarch string) (downloads.Artifact, bool)
	transport http.RoundTripper // nil: the production transport
	progress  time.Duration
}

type toolDownload struct {
	artifact downloads.Artifact
	done     chan struct{}
	cancel   context.CancelFunc
	waiters  int
	use      bool
	useActor api.Actor
	err      error
}

func (s *Engine) artifactFor(tool string) (downloads.Artifact, bool) {
	lookup := downloads.Lookup
	if s.downloadHooks.lookup != nil {
		lookup = s.downloadHooks.lookup
	}
	return lookup(tool, runtime.GOOS, runtime.GOARCH)
}

func knownTool(tool string) bool {
	_, ok := toolVersionArgs[tool]
	return ok
}

// DownloadTool downloads, verifies and installs the pinned release of tool, and
// selects it unless use is false. A second request for the same tool joins the
// running download. The download is cancelled by DownloadCancel, by daemon
// shutdown, or when every waiting request has gone away.
func (s *Engine) DownloadTool(ctx context.Context, actor api.Actor, tool string, use bool) ([]api.ToolInfo, error) {
	if !knownTool(tool) {
		return nil, api.NewError(api.Unknown, fmt.Sprintf("unsupported tool %q", tool))
	}
	artifact, ok := s.artifactFor(tool)
	if !ok {
		return nil, api.NewError(api.NotFound, fmt.Sprintf("no %s download is available for %s/%s", tool, runtime.GOOS, runtime.GOARCH))
	}
	if err := artifact.Validate(); err != nil {
		return nil, api.NewError(api.Internal, err.Error())
	}
	s.downloadMu.Lock()
	d := s.downloads[tool]
	if d == nil {
		jobCtx, cancel := context.WithCancel(context.Background())
		d = &toolDownload{artifact: artifact, done: make(chan struct{}), cancel: cancel}
		if s.downloads == nil {
			s.downloads = map[string]*toolDownload{}
		}
		s.downloads[tool] = d
		go s.runDownload(jobCtx, tool, d)
	}
	d.waiters++
	if use && !d.use {
		d.use, d.useActor = true, actor
	}
	s.downloadMu.Unlock()

	var err error
	select {
	case <-d.done:
		err = d.err
	case <-ctx.Done():
		err = ctx.Err()
	case <-s.done:
		err = api.NewError(api.Stopped, "daemon is shutting down")
	}
	s.downloadMu.Lock()
	d.waiters--
	if d.waiters == 0 {
		d.cancel() // nobody is waiting any more
	}
	s.downloadMu.Unlock()

	result := audit.ResultOK
	if err != nil {
		result = audit.ResultFailed
	}
	if auditErr := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.ToolDownload,
		Tool: tool, ToolVersion: artifact.Version, Result: result}); auditErr != nil && err == nil {
		err = auditErr
	}
	if err != nil {
		return nil, err
	}
	return s.Tools(), nil
}

// DownloadCancel stops tool's running download, if any.
func (s *Engine) DownloadCancel(tool string) error {
	if !knownTool(tool) {
		return api.NewError(api.Unknown, fmt.Sprintf("unsupported tool %q", tool))
	}
	s.downloadMu.Lock()
	if d := s.downloads[tool]; d != nil {
		d.cancel()
	}
	s.downloadMu.Unlock()
	return nil
}

func (s *Engine) runDownload(ctx context.Context, tool string, d *toolDownload) {
	go func() { // daemon shutdown cancels the job
		select {
		case <-s.done:
			d.cancel()
		case <-ctx.Done():
		}
	}()
	a := d.artifact
	path, err := s.install(ctx, tool, a)
	if err == nil {
		s.downloadMu.Lock()
		use, actor := d.use, d.useActor
		s.downloadMu.Unlock()
		if use {
			_, err = s.SetToolPathFor(actor, tool, path)
		} else {
			s.notify()
		}
	}
	final := api.ToolDownloadProgress{Tool: tool, Version: a.Version, State: api.DownloadDone, Received: a.Size, Total: a.Size}
	switch {
	case err != nil && ctx.Err() != nil:
		final.State, final.Received = api.DownloadCancelled, 0
		err = api.NewError(api.Stopped, tool+" download was cancelled")
	case err != nil:
		final.State, final.Received, final.Error = api.DownloadFailed, 0, err.Error()
	}
	s.downloadMu.Lock()
	d.err = err
	delete(s.downloads, tool)
	s.downloadMu.Unlock()
	d.cancel()
	s.emit(api.Event{Type: api.EventToolDownload, Download: &final})
	close(d.done)
}

// install returns the verified executable for a, downloading it when needed.
func (s *Engine) install(ctx context.Context, tool string, a downloads.Artifact) (string, error) {
	root, err := platform.ToolsDir()
	if err != nil {
		return "", api.NewError(api.Internal, "tools directory: "+err.Error())
	}
	toolDir := filepath.Join(root, tool)
	if err := os.MkdirAll(toolDir, 0o700); err != nil {
		return "", api.NewError(api.Internal, "tools directory: "+err.Error())
	}
	versionDir := filepath.Join(toolDir, a.Version)
	exe := filepath.Join(versionDir, platform.ExecutableName(tool))
	if verifyInstalled(versionDir, exe, a) {
		return exe, nil
	}
	cleanStaleDownloads(toolDir)
	if _, err := os.Lstat(versionDir); err == nil {
		// Present but unverifiable: replace it, never trust it.
		if err := os.RemoveAll(versionDir); err != nil {
			return "", api.NewError(api.Internal, "remove unverified "+a.Version+": "+err.Error())
		}
	}

	partial := filepath.Join(toolDir, "."+a.Version+".partial")
	if err := s.fetch(ctx, tool, a, partial); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	s.emitProgress(tool, a, api.DownloadVerifying, a.Size)
	tmp, err := os.MkdirTemp(toolDir, ".tmp-"+a.Version+"-")
	if err != nil {
		_ = os.Remove(partial)
		return "", api.NewError(api.Internal, err.Error())
	}
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(tmp)
		_ = os.Remove(partial)
		return "", err
	}
	target := filepath.Join(tmp, platform.ExecutableName(tool))
	switch a.Format {
	case downloads.FormatRaw:
		if err := os.Rename(partial, target); err != nil {
			return fail(api.NewError(api.Internal, err.Error()))
		}
	case downloads.FormatTGZ:
		if err := extractMember(partial, a.Member, target); err != nil {
			return fail(err)
		}
		_ = os.Remove(partial)
	}
	sum, err := fileSHA256(target)
	if err != nil {
		return fail(api.NewError(api.Internal, err.Error()))
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return fail(api.NewError(api.Internal, err.Error()))
	}
	if err := os.WriteFile(filepath.Join(tmp, installSidecar), []byte(a.SHA256+"\n"+sum+"\n"), 0o600); err != nil {
		return fail(api.NewError(api.Internal, err.Error()))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	out, err := platform.RunTool(ctx, target, toolVersionArgs[tool]...)
	if err != nil {
		return fail(api.NewError(api.Internal, fmt.Sprintf("downloaded %s does not run: %v", tool, err)))
	}
	if !mentionsVersion(string(out), a.Version) {
		return fail(api.NewError(api.ChecksumMismatch, fmt.Sprintf("downloaded %s does not report version %s", tool, a.Version)))
	}
	if err := os.Rename(tmp, versionDir); err != nil {
		return fail(api.NewError(api.Internal, err.Error()))
	}
	return exe, nil
}

// verifyInstalled accepts an existing version directory only when its
// executable hashes to what this artifact produced: the manifest hash for raw
// files, or the member hash recorded at install for archives.
func verifyInstalled(versionDir, exe string, a downloads.Artifact) bool {
	info, err := os.Lstat(versionDir)
	if err != nil || !info.IsDir() {
		return false
	}
	if info, err := os.Lstat(exe); err != nil || !info.Mode().IsRegular() {
		return false
	}
	sum, err := fileSHA256(exe)
	if err != nil {
		return false
	}
	if a.Format == downloads.FormatRaw {
		return sum == a.SHA256
	}
	sidecar, err := os.ReadFile(filepath.Join(versionDir, installSidecar))
	if err != nil {
		return false
	}
	lines := strings.Fields(string(sidecar))
	return len(lines) == 2 && lines[0] == a.SHA256 && lines[1] == sum
}

// cleanStaleDownloads removes what an interrupted download left behind. Only
// names this code creates are touched.
func cleanStaleDownloads(toolDir string) {
	entries, _ := os.ReadDir(toolDir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && (strings.HasSuffix(name, ".partial") || strings.HasPrefix(name, ".tmp-")) {
			_ = os.RemoveAll(filepath.Join(toolDir, name))
		}
	}
}

func (s *Engine) downloadClient() *http.Client {
	transport := s.downloadHooks.transport
	if transport == nil {
		dialer := &net.Dialer{Timeout: downloadConnectTimeout, KeepAlive: 30 * time.Second}
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   downloadConnectTimeout,
			ResponseHeaderTimeout: downloadConnectTimeout,
			ForceAttemptHTTP2:     true,
			IdleConnTimeout:       30 * time.Second,
		}
	}
	return &http.Client{
		Transport: transport, // no Timeout: large files on slow links must finish
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("refusing a redirect to a non-HTTPS URL")
			}
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

// fetch streams the artifact to partial, hashing as it goes, and fails unless
// the body is exactly a.Size bytes with hash a.SHA256.
func (s *Engine) fetch(ctx context.Context, tool string, a downloads.Artifact, partial string) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return api.NewError(api.Internal, err.Error())
	}
	version := s.version
	if version == "" {
		version = "dev"
	}
	req.Header.Set("User-Agent", "devt-tunnel/"+version)
	req.Header.Set("Accept-Encoding", "identity")
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return api.NewError(api.Internal, err.Error())
	}
	defer file.Close()
	s.emitProgress(tool, a, api.DownloadDownloading, 0)
	resp, err := s.downloadClient().Do(req)
	if err != nil {
		return networkError(ctx, tool, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return api.NewError(api.Network, fmt.Sprintf("download %s: server returned %s", tool, resp.Status))
	}
	if resp.ContentLength > a.Size {
		return api.NewError(api.ChecksumMismatch, fmt.Sprintf("download %s: server announced %d bytes, expected %d", tool, resp.ContentLength, a.Size))
	}
	stall := time.AfterFunc(downloadStallTimeout, func() { cancel(errStalled) })
	defer stall.Stop()
	hash := sha256.New()
	gap := s.downloadHooks.progress
	if gap == 0 {
		gap = downloadProgressGap
	}
	var received int64
	last := time.Now()
	buf := make([]byte, 64<<10)
	body := io.LimitReader(resp.Body, a.Size+1)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			stall.Reset(downloadStallTimeout)
			received += int64(n)
			if received > a.Size {
				return api.NewError(api.ChecksumMismatch, fmt.Sprintf("download %s: body is larger than the expected %d bytes", tool, a.Size))
			}
			hash.Write(buf[:n])
			if _, err := file.Write(buf[:n]); err != nil {
				return api.NewError(api.Internal, err.Error())
			}
			if time.Since(last) >= gap {
				last = time.Now()
				s.emitProgress(tool, a, api.DownloadDownloading, received)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return networkError(ctx, tool, readErr)
		}
	}
	if received < a.Size {
		return api.NewError(api.Network, fmt.Sprintf("download %s: connection closed after %d of %d bytes", tool, received, a.Size))
	}
	if err := file.Close(); err != nil {
		return api.NewError(api.Internal, err.Error())
	}
	if hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return api.NewError(api.ChecksumMismatch, fmt.Sprintf("download %s: the file did not match the expected checksum and was deleted", tool))
	}
	return nil
}

var errStalled = errors.New("no data received for 60 seconds")

func networkError(ctx context.Context, tool string, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
		err = cause
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	return api.NewError(api.Network, fmt.Sprintf("download %s: %v", tool, err))
}

func (s *Engine) emitProgress(tool string, a downloads.Artifact, state string, received int64) {
	s.emit(api.Event{Type: api.EventToolDownload, Download: &api.ToolDownloadProgress{
		Tool: tool, Version: a.Version, State: state, Received: received, Total: a.Size}})
}

// extractMember writes the single regular-file entry named member to target.
// Any other entry is skipped without being written; an entry with the member's
// name that is not a regular file, or appears twice, fails the install.
func extractMember(archive, member, target string) error {
	bad := func(msg string) error { return api.NewError(api.ChecksumMismatch, "archive: "+msg) }
	f, err := os.Open(archive)
	if err != nil {
		return api.NewError(api.Internal, err.Error())
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return bad("not gzip data")
	}
	tr := tar.NewReader(&limitedReader{r: gz, left: maxDecompressed})
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad(err.Error())
		}
		name := strings.TrimPrefix(h.Name, "./")
		if name != member {
			continue
		}
		if found {
			return bad("member appears twice")
		}
		if h.Typeflag != tar.TypeReg {
			return bad("member is not a regular file")
		}
		if h.Size <= 0 || h.Size > maxExtractedMember {
			return bad("member size is out of range")
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return api.NewError(api.Internal, err.Error())
		}
		n, copyErr := io.Copy(out, io.LimitReader(tr, h.Size))
		closeErr := out.Close()
		if copyErr != nil || n != h.Size {
			return bad("member is truncated")
		}
		if closeErr != nil {
			return api.NewError(api.Internal, closeErr.Error())
		}
		found = true
	}
	if !found {
		return bad("member " + member + " is missing")
	}
	return nil
}

type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, errors.New("archive is too large when decompressed")
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func mentionsVersion(text, version string) bool {
	return regexp.MustCompile(`(^|[^0-9.])` + regexp.QuoteMeta(version) + `($|[^0-9.])`).MatchString(text)
}

// downloadOffer is ToolInfo.Download: set when an artifact exists and the
// selected binary does not report that version.
func (s *Engine) downloadOffer(tool, selectedVersion string) *api.ToolDownloadOffer {
	a, ok := s.artifactFor(tool)
	if !ok || a.Validate() != nil {
		return nil
	}
	if selectedVersion != "" && mentionsVersion(selectedVersion, a.Version) {
		return nil
	}
	return &api.ToolDownloadOffer{Version: a.Version, Size: a.Size}
}

// RemoveDownloadedTool deletes one managed version directory. It refuses while
// a running tunnel uses that binary, and clears a matching override first.
func (s *Engine) RemoveDownloadedTool(actor api.Actor, tool, version string) ([]api.ToolInfo, error) {
	if !knownTool(tool) {
		return nil, api.NewError(api.Unknown, fmt.Sprintf("unsupported tool %q", tool))
	}
	if !downloads.ValidVersion(version) {
		return nil, api.NewError(api.Unknown, "version must be a dotted release number such as 2026.11.0")
	}
	root, err := platform.ToolsDir()
	if err != nil {
		return nil, api.NewError(api.Internal, "tools directory: "+err.Error())
	}
	dir := filepath.Join(root, tool, version)
	if !within(root, dir) {
		return nil, api.NewError(api.Unknown, "version is outside the tools directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return nil, api.NewError(api.NotFound, fmt.Sprintf("%s %s is not downloaded", tool, version))
	}
	if real, err := filepath.EvalSymlinks(dir); err != nil || !within(realPath(root), real) {
		return nil, api.NewError(api.Unknown, "version is outside the tools directory")
	}
	s.downloadMu.Lock()
	busy := s.downloads[tool] != nil
	s.downloadMu.Unlock()
	if busy {
		return nil, api.NewError(api.Conflict, fmt.Sprintf("a %s download is in progress", tool))
	}
	for _, st := range s.Statuses() {
		if st.State != supervisor.StateStopped && st.State != supervisor.StateFailed && st.ToolPath != "" && within(dir, st.ToolPath) {
			return nil, api.NewError(api.Conflict, fmt.Sprintf("a running tunnel uses %s %s; stop it first", tool, version))
		}
	}
	if override := s.tools.Override(tool); override != "" && within(dir, override) {
		if _, err := s.SetToolPathFor(actor, tool, ""); err != nil {
			return nil, err
		}
	}
	removeErr := os.RemoveAll(dir)
	result := audit.ResultOK
	if removeErr != nil {
		result = audit.ResultFailed
	}
	if err := s.recordAudit(audit.Event{Actor: auditActor(actor), Action: audit.ToolRemove, Tool: tool, ToolVersion: version, Result: result}); err != nil && removeErr == nil {
		removeErr = err
	}
	s.probeCache.Range(func(k, _ any) bool {
		if within(dir, k.(probeKey).path) || within(realPath(dir), k.(probeKey).path) {
			s.probeCache.Delete(k)
		}
		return true
	})
	s.notify()
	if removeErr != nil {
		return nil, api.NewError(api.Internal, removeErr.Error())
	}
	return s.Tools(), nil
}

// within reports whether path is dir or below it, lexically.
func within(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// managedVersion is the version directory of a binary devt-tunnel downloaded,
// or "" for any other path (paths themselves are never audited).
func managedVersion(tool, path string) string {
	root, err := platform.ToolsDir()
	if err != nil || path == "" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Join(root, tool), path)
	if err != nil {
		return ""
	}
	version, rest, ok := strings.Cut(rel, string(filepath.Separator))
	if !ok || rest != platform.ExecutableName(tool) || !downloads.ValidVersion(version) {
		return ""
	}
	return version
}
