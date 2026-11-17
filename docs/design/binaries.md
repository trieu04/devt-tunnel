# Binaries: selection and download

Status: implemented (cloudflared); acceptance open. B1, B2 and B4 accepted; B3 (ngrok) still open. Work is tracked in the [roadmap](../roadmap.md#setup-binaries).

**Goal:** a person goes from "installed devt-tunnel" to "a tunnel is running" without leaving the app, even when the tunnel's tool is not installed yet. Choosing among several installs takes one click, not a typed path.

## Problem

Tunnels run the system `ssh`, `cloudflared` and `ngrok` ([architecture.md](../architecture.md)). Today, Settings → Binaries shows one resolved path per tool and a free-text box for an absolute path. That leaves three gaps:

1. **No choice between installs.** `Tools.Locate` returns the first match on the merged PATH. A person with both Homebrew and a manual `cloudflared` cannot see the second one, and has to type its path.
2. **A missing tool is a dead end.** A first-time user who pastes a `cloudflared tunnel --url …` command gets `not_found` and has to leave the app to install the tool.
3. **No file picker.** Typing an absolute path is error-prone, especially on Windows.

## Scope

In scope:

- List every executable found for a tool, with version and origin, and let the person pick one, pick **Auto**, or browse for a file.
- Download a **pinned** `cloudflared` release on explicit request, verify it, and select it.
- The same through the CLI.

Out of scope:

- Downloading `ssh`. It ships with macOS, Linux distributions and Windows 10/11. The page links to the OS instructions instead.
- Automatic or background downloads, and automatic updates of downloaded binaries.
- Bundling binaries in installers (unchanged).
- `ngrok` download in the first version; see [open questions](#open-questions).

## Principles

- **Only a person starts a download.** A button in the app or `devt-tunnel tools download`. Never the daemon on its own, never MCP.
- **The daemon only fetches URLs compiled into it.** No method takes a URL or hash. A compromised client can at most request "cloudflared", and the result is still checked against a hash shipped in the signed devt-tunnel binary.
- **No new resolution rule.** Selecting a binary, downloaded or not, writes the existing `[tools]` override in `settings.toml`. Clearing it returns to Auto. `Tools.Locate` keeps its order.
- **Running tunnels are not touched.** A new selection applies on the next start.

## Model

### Candidates

A candidate is one executable that could serve a tool.

```go
// internal/api/types.go
type ToolCandidate struct {
	Path     string `json:"path"`     // absolute, as it will be stored
	Resolved string `json:"resolved"` // after EvalSymlinks; used to merge duplicates
	Version  string `json:"version"`  // first line of the version command; empty on error
	Origin   string `json:"origin"`   // path | homebrew | scoop | winget | system | downloaded | custom
	Auto     bool   `json:"auto"`     // what Auto would pick right now
	Selected bool   `json:"selected"` // equals the settings override
	Error    string `json:"error,omitempty"`
}
```

Discovery, in `internal/platform`:

- Walk every directory of `Tools.PATH()`, not only until the first hit.
- Add every version directory under the managed tools directory.
- Add the current override if it is outside both, with origin `custom`.
- Merge entries whose `Resolved` paths match (Homebrew's `/opt/homebrew/bin/cloudflared` points into `Cellar`), keeping the first `Path`.
- `Origin` comes from the directory: `/opt/homebrew`, `/usr/local/Cellar` → `homebrew`; `scoop\shims` → `scoop`; `WinGet\Links` → `winget`; `/usr/bin`, `System32\OpenSSH` → `system`; managed directory → `downloaded`; otherwise `path`.
- Version probes reuse `platform.RunTool` with its timeout, run in parallel, and are cached by `(resolved path, size, mtime)` so opening the page twice does not run each binary twice.

### Managed tools directory

`platform.ToolsDir()`:

| OS | Path |
|---|---|
| macOS | `~/Library/Application Support/devt-tunnel/tools` |
| Linux | `$XDG_DATA_HOME/devt-tunnel/tools` (default `~/.local/share/…`) |
| Windows | `%LOCALAPPDATA%\devt-tunnel\tools` |

`DEVT_TUNNEL_DATA_DIR` overrides it for tests. Layout: `tools/<tool>/<version>/<executable>`. Directories are `0700`, executables `0755`. Nothing outside this directory is ever written or deleted by this feature.

### Manifest

A generated Go file, `internal/platform/downloads/manifest_gen.go`, lists one release per tool and platform:

```go
type Artifact struct {
	Tool, Version  string
	GOOS, GOARCH   string
	URL            string // https only
	SHA256         string // of the downloaded file
	Size           int64  // exact; the download aborts past it
	Format         string // raw | tgz
	Member         string // file inside the archive, for tgz
}
```

Initial targets, from `github.com/cloudflare/cloudflared/releases`: `darwin/arm64` and `darwin/amd64` (`.tgz`), `linux/amd64` and `linux/arm64` (raw), `windows/amd64` (`.exe`, raw). A platform without an entry shows no download button.

`task downloads:manifest` fetches the chosen release, downloads each asset, computes the hash and size, and rewrites the file. The diff is reviewed like code. [release.md](../release.md) gains a step: "refresh the binary manifest, or note why not". Hashes are computed by us, not copied from the release page, so a tampered release body cannot inject one.

## Download

Runs in the daemon (`internal/engine`), since the daemon spawns the tools and serves both the app and the CLI.

1. Look up the artifact for `runtime.GOOS/GOARCH`. None → `not_found`.
2. If the version directory already exists and its executable hashes correctly, skip to step 6.
3. One download per tool at a time; a second request joins the first.
4. `GET` with `http.ProxyFromEnvironment`, a 30 s connect timeout, no overall timeout, HTTPS-only redirects, and a generic `User-Agent: devt-tunnel/<version>`. Stream to `tools/<tool>/.<version>.partial`, hashing as it goes. Abort if the body exceeds `Size`. Publish progress at most every 250 ms.
5. On hash mismatch, delete the partial file and fail with `checksum_mismatch`. Otherwise extract: for `tgz`, read only the entry named `Member`, reject links and any other entry type, and write it to a temporary file in the same directory. `chmod 0755`, run the version command once, then rename the temporary directory to `<version>`.
6. Unless the request says `use: false`, set the override to the new path, as `tool.setPath` does.
7. Emit the final event and return the refreshed `ToolInfo`.

Cancelling closes the request and removes the partial file. A daemon exit mid-download leaves only a `.partial` file, which the next download for that tool deletes.

Binaries written by a Go HTTP client carry no quarantine attribute on macOS and no Mark of the Web on Windows, so Gatekeeper and SmartScreen do not prompt. That is why the hash check is required: it is the only verification.

## Local API

All new methods are GUI and CLI only. `mcpMethodAllowed` stays unchanged; a test asserts that none of them is allowed.

| Method | Params | Result | Notes |
|---|---|---|---|
| `tool.list` | – | `ToolInfo[]` | Adds `origin` and `download?: {version, size}` when the manifest has a release newer than, or in place of, the selection |
| `tool.candidates` | `{tool}` | `ToolCandidate[]` | Read-only; may take a second while probing |
| `tool.setPath` | `{tool, path}` | `ToolInfo[]` | Unchanged; `""` means Auto |
| `tool.download` | `{tool, use}` | `ToolInfo[]` | Returns when finished; progress through events |
| `tool.downloadCancel` | `{tool}` | – | No-op if nothing is running |
| `tool.removeDownloaded` | `{tool, version}` | `ToolInfo[]` | Refuses with `conflict` while a running tunnel uses it; clears the override first if it is selected |

Event `tool.download`: `{tool, version, state: "downloading"|"verifying"|"done"|"failed"|"cancelled", received, total, error?}`. It is carried in the event's `download` field, never sent to MCP clients, and forwarded by the desktop host as the Wails event `tool:download`.

Errors: `not_found` (no artifact for this platform), `checksum_mismatch` (new, exit code 1), `network` (new, exit code 1, wraps the transport error), `conflict`.

`audit.jsonl` records `tool.download`, `tool.setPath` and `tool.removeDownloaded` with actor, `tool` and, for managed binaries, `tool_version`, since each changes which program the daemon runs. Paths are not recorded: the audit schema holds only validated identifiers, never free-form text.

## CLI

```text
devt-tunnel tools                               # unchanged table, adds ORIGIN
devt-tunnel tools candidates cloudflared        # PATH, VERSION, ORIGIN, AUTO, SELECTED
devt-tunnel tools set cloudflared <path>|--clear  # unchanged
devt-tunnel tools download cloudflared [--no-use]
devt-tunnel tools remove cloudflared <version>
```

`download` prints a progress line on stderr when it is a TTY, and the refreshed tool list on stdout; `--json` prints only the final result. Ctrl-C sends `tool.downloadCancel`. `devt-tunnel doctor` suggests `devt-tunnel tools download cloudflared` when `cloudflared` is missing and the platform has an artifact.

## UI

### Settings → Binaries

One card per tool. The header shows what tunnels will run; the body is a radio list. A selection applies immediately, so there is no Save button.

```text
┌ cloudflared ─────────────────────────────────────────────────────────┐
│ 2026.10.0 · Homebrew                                                  │
│                                                                       │
│ ( ) Auto                        uses /opt/homebrew/bin/cloudflared    │
│ (•) /opt/homebrew/bin/cloudflared          2026.10.0   Homebrew       │
│ ( ) ~/…/tools/cloudflared/2026.9.1/…       2026.9.1    Downloaded  ⋯  │
│ ( ) Custom…                                                           │
│                                                                       │
│ ⤓ Download cloudflared 2026.9.1 · 38 MB      from GitHub (Cloudflare)  │
└───────────────────────────────────────────────────────────────────────┘
```

- **Auto** is selected when no override is set; its hint shows what Auto resolves to now.
- **Custom…** expands a path field with **Browse…** (a native file dialog from a new host method, `Service.PickExecutable(tool)`) and **Use**. Validation errors from `tool.setPath` appear under the field. A custom path that is already set appears as its own row.
- **⋯** on a downloaded row: **Reveal in Finder/Explorer**, **Remove**. Remove is disabled with a tooltip while a running tunnel uses it.
- A candidate whose version probe failed shows the error in place of the version and can still be selected; the header then shows a warning.
- Paths are middle-truncated, with the full path in a tooltip and copyable.

States of the download row:

| State | Shows |
|---|---|
| Available | Secondary button **Download cloudflared ⟨version⟩ · ⟨size⟩**, source note |
| Downloading | Progress bar, `12.4 / 38 MB`, **Cancel** |
| Verifying | Indeterminate bar, "Verifying checksum…" |
| Failed | Error text and **Retry**. Mismatch: "The download didn't match the expected checksum and was deleted." |
| Up to date | Hidden when the selected binary is the manifest version |
| Newer available | When a downloaded binary is selected and the manifest is newer: "⟨version⟩ is available" with the button |
| Unsupported platform | Hidden |

The ssh card has no download row. When ssh is missing it says "OpenSSH is not installed" with a link: on Windows, "Settings → Optional features → OpenSSH Client"; on Linux, the distribution's `openssh-client` package.

The ngrok card, until B3 is decided, shows an install hint instead (`brew install ngrok`, `winget install ngrok.ngrok`, `snap install ngrok`) with a copy button and a link to the official download page.

### Empty state

When a tool has no candidates the card leads with the problem:

```text
┌ cloudflared ─────────────────────────────────────────────────────────┐
│ ⚠ cloudflared isn't installed                                         │
│ Cloudflare tunnels need it.                                           │
│                                                                       │
│ [⤓ Download cloudflared 2026.9.1]  or  brew install cloudflared  ⧉    │
│ Already installed somewhere else? Choose file…                        │
└───────────────────────────────────────────────────────────────────────┘
```

Download is the primary button only here.

### After a change

If running tunnels use the previous binary, a banner under the card says: "2 running tunnels still use the previous cloudflared. **Restart them**." The button restarts exactly those tunnels. It disappears when none remain.

### First-use setup

Setup happens where the missing tool is first needed, not in a separate wizard:

1. The person picks the Cloudflared type, or pastes a `cloudflared …` command, in the editor.
2. The editor's binary check reports `cloudflared isn't installed` with **Download** inline (and the install hint as the secondary option).
3. Download runs in place with progress; on success the check turns green and the downloaded binary is selected.
4. **Save & start** works immediately. No restart of the app or daemon is needed.

The same applies to `devt-tunnel expose 3000 --via cloudflared` on a TTY: when the tool is missing and an artifact exists, the error names `devt-tunnel tools download cloudflared` as the fix. The CLI never downloads without that explicit command.

Target: from a fresh install, a Cloudflare quick tunnel is running in under a minute on a normal connection, with no terminal.

### Entry points elsewhere

- The editor's "binary not found" finding and the tunnel detail's **Tool: Not found** row link to Settings → Binaries with that tool's card focused (`initial="binaries"`, `item="cloudflared"`). When an artifact exists, the finding offers **Download** inline, which calls the same method and shows progress in the finding.
- Settings → General: replace "The optional GitHub release check is devt-tunnel's only network request of its own." with "devt-tunnel only connects to the network for the optional release check and for binaries you choose to download."

## Testing

- **Manifest:** every entry is HTTPS, has a 64-hex hash and a positive size, `tgz` entries name a member, and every release target in `Taskfile.yml` has a `cloudflared` entry or is listed as unsupported.
- **Download, against `httptest` with an injected manifest:** success; hash mismatch deletes the file; body larger than `Size`; truncated body; redirect to `http://`; cancel; two concurrent requests share one download; leftover `.partial` is cleaned; `tgz` with an extra file, a symlink member and `../` names extracts nothing else; `use: false` leaves the override.
- **Candidates:** duplicates through symlinks are merged; origin detection per OS (table-driven with fake directories); a binary whose version command hangs returns within the probe timeout.
- **Policy:** MCP calls to every new method fail with the normal not-allowed error.
- **Remove:** refused while a tunnel runs the binary; clears a matching override.
- **Frontend:** each download state renders; selecting a row calls `SetToolPath` once; the restart banner counts only tunnels whose `toolPath` differs from the new selection.
- **Manual, per OS:** download and start a quick tunnel on a clean machine; on macOS confirm no Gatekeeper prompt; on Windows confirm no SmartScreen prompt and that the uninstaller removes `tools`.

## Plan

1. **Select** (no network): candidates, radio list, Browse, restart banner, `tools candidates`. Useful on its own and closes gaps 1 and 3.
2. **Download cloudflared:** manifest and generator, engine download, events, CLI, UI states, General text, release step.
3. **ngrok**, if B3 is accepted.

## Open questions

- **ngrok source.** Its public download URLs (`bin.equinox.io/…/ngrok-v3-stable-…`) are not versioned, so a pinned hash breaks whenever ngrok publishes. Options: find a versioned URL, accept trust-on-first-use (weaker than everything else here), or keep the install hint. Also check that ngrok's terms allow an app to fetch it on the user's behalf.
- **Retention.** Keep every downloaded version until removed (proposed), or keep only the selected one and the previous one.
- **Linux packages.** deb/rpm users may expect the package manager; the button could be hidden when the app was installed from a package.

## Decisions

| # | Decision | Status | Consequence |
|---|---|---|---|
| B1 | Allow on-demand download of `cloudflared` started by a person, to make setup fast; bundling stays out of scope | Accepted 2026-11-17 | [product.md](../product.md) scope line changed; the General text and README change when it ships |
| B2 | Hashes are pinned in a manifest compiled into devt-tunnel | Implemented 2026-11-17 | Downloads lag upstream until the next devt-tunnel release; each release refreshes the manifest |
| B3 | ngrok download is deferred | Proposed | ngrok keeps an install hint |
| B4 | Selection reuses the `[tools]` override | Accepted 2026-11-17 | No settings migration; Auto is the absence of an override |
