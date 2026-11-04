# Platform

Status: **adopted** for the code base. Automated builds target all three operating systems, but the native desktop matrix remains incomplete; only the existing macOS evidence below has been observed by a person. Research date: 2026-11-03; implementation status updated 2026-11-04.

Targets: macOS, Windows and Linux.

## Recommendation

| Layer | Choice |
|---|---|
| App shell | Wails v3.1.0, pinned (Go host, system WebView) |
| Core | Go packages with no GUI dependency: model, argv builder, supervisor, checks |
| UI | React + TypeScript + Vite, built with pnpm |
| Process model | One app process today. Accepted: an on-demand daemon with the GUI as a client ([design](design/control-plane.md), phases 14–17); a small separate askpass helper in both |
| Secrets | `zalando/go-keyring` behind an interface; never a plaintext fallback |
| Linux packages | deb and rpm first, AppImage later; no Flatpak or strict Snap |

**Tauri 2 with a Rust core** remains the fallback if the validation checks fail.

The main reason for Wails is fit: most of devt-tunnel is supervising processes and integrating with the desktop (tray, global shortcut, windows), and Wails v3 ships those desktop features first-party. The main risk is maturity: v3.0.0 is only days old, which is why the core stays independent of it and the spike comes first.

## What UniClipboard teaches

[UniClipboard](https://github.com/UniClipboard/UniClipboard) (about 2k stars, v1.6.1 released 2026-11-03) is a tray-first desktop app on the same three platforms.

| Observation | Relevance to devt-tunnel |
|---|---|
| Moved its desktop host from Tauri to Wails v3 in October 2026 and kept the React frontend ([#1840](https://github.com/UniClipboard/UniClipboard/pull/1840), [#1912](https://github.com/UniClipboard/UniClipboard/pull/1912)) | A real tray app ships on Wails v3, and a web UI survives a shell swap |
| Pins `v3.0.0-beta.28` and records a capability audit against that pinned source | Pin one version and audit it, rather than trusting the feature list |
| Host code still needs per-OS work: 82 of 159 top-level Go files in `apps/gui-go` are `_darwin`, `_linux`, `_windows` or `_other` variants | The framework does not remove platform work; budget for it |
| Tray menu behavior still has open acceptance gaps on macOS ([#1906](https://github.com/UniClipboard/UniClipboard/issues/1906)) | Keep tray menus simple and test them on a real desktop |
| Eight architecture documents cover Linux AppImage issues alone (bundled WebKit, bubblewrap, proxy, host helpers) | AppImage is expensive; do not start there |
| Strips AppImage variables from the environment of host programs it launches (`host_helper_env_linux.go`) | devt-tunnel spawns host programs for every tunnel, so this applies directly |
| Separates test evidence: a container with Xvfb is explicitly "not native desktop acceptance" | Adopt the same rule for Wayland, tray and shortcut claims |
| GUI and CLI are clients of a daemon, motivated by a mode where the GUI exits and the daemon stays ([ADR-008](https://github.com/UniClipboard/UniClipboard/blob/main/docs/architecture/adr-008-uniclipd-split-gui-as-client.md)) | Originally not copied. Now adopted for a different reason: the CLI and agents need a shared registry with no desktop. ⌘Q still stops all tunnels |
| Rust engine for P2P, crypto and encrypted storage | **Do not copy.** devt-tunnel has none of that; a second language would add cost only |
| Relay, sync engine and Snap/Flatpak channels | **Do not copy.** Out of scope, and sandboxes conflict with spawning host binaries |

**Limit of this evidence:** the documents and PRs I read do not state *why* UniClipboard left Tauri. The only concrete hint is a vendored `tao` patch for a macOS window zoom fix ([#1841](https://github.com/UniClipboard/UniClipboard/pull/1841)). Treat the migration as proof that Wails v3 is workable, not that it is better than Tauri. UniClipboard also pins a beta, so it is not evidence for the 3.x stable line.

## Options

| Criterion | Wails v3 (Go) | Tauri 2 (Rust) | Electron |
|---|---|---|---|
| Latest release seen | 3.1.0 (2026-11-01) | 2.12.5 (2026-10-21) | 45.2.1 (2026-10-31) |
| Stable since | **2026-10-26**, after about 4 years pre-release | October 2024 | Long established |
| GitHub stars | 37k | 112k | 124k |
| Tray, multi-window, single instance | Built in | Built in or official plugins | Built in |
| Global shortcut, Wayland | Built in, uses the XDG portal ([#5686](https://github.com/wailsapp/wails/pull/5686)); not verified here | Official plugin, open Wayland bug on GNOME 48 ([#3267](https://github.com/tauri-apps/plugins-workspace/issues/3267)) | Built in; not assessed |
| Linux WebView | GTK4 + WebKitGTK 6.0 (GTK3 removed in v3.1) | WebKitGTK; open Nvidia and Wayland reports ([#9394](https://github.com/tauri-apps/tauri/issues/9394), [#10702](https://github.com/tauri-apps/tauri/issues/10702)) | Bundled Chromium, consistent everywhere |
| Rendering differences across OSes | Yes | Yes | No |
| Process supervision | `os/exec`, goroutines, `x/sys` | `tokio::process`, `windows` crate | `child_process`; Job Objects need a native module |
| Idle footprint for an always-on tray app | Low | Low | High; one blog reports 150 MB+ for hello-world (not measured here) |
| Updater | Built its own on beta.28; 3.1.0 has an updater provider, not evaluated | Official plugin | Mature ecosystem |
| API stability risk | Higher: weekly releases, young stable line | Lower; note that v3 alpha is in progress | Low, frequent major versions |

Rejected without detailed comparison:

- **Three native apps:** three UIs for a small project.
- **Flutter, Compose Multiplatform:** the mockups and search-heavy list UI suit web technology, and tray and global shortcuts would rely on third-party plugins.
- **Electron:** it is the safe fallback for rendering problems, but a large runtime is hard to justify for a tool that shows a list and mostly idles in the tray.

## Architecture

```text
┌─ devt-tunnel (one process) ────────────────────────────────────┐
│  frontend/ React UI: main, quick launch, editor, vault        │
│        ▲ generated typed bindings + events                    │
│  internal/host: Wails services, tray, shortcut, windows       │
│        ▼ internal/api shared types                           │
│  internal/engine: registry, prompts, settings, core wiring    │
│        ▼                                                      │
│  internal/core: model · config · argv builder · importer      │
│                 supervisor · readiness · checks · profiles    │
│        ▼ interfaces                                           │
│  internal/platform: proc · env · dirs · secrets · userauth    │
└───────┬──────────────────────────────────────▲────────────────┘
        │ spawn                                │ local IPC, per-spawn token
        ▼                                      │
   ssh · cloudflared · ngrok · command ──► devt-tunnel-askpass
```

```text
main.go                   Wails entry point; embeds frontend/dist (Wails build tasks expect it at the root)
cmd/devt-tunnel-askpass/    helper; no GUI dependency
internal/api/             shared client types and events; no Wails
internal/engine/           registry, prompts, settings and core wiring; no Wails
internal/core/            model, config, command, checks, supervisor; no Wails, no OS-specific code
internal/askpass/         prompt protocol shared by the app and the helper
internal/platform/        process, env, paths, secrets; *_unix.go, *_windows.go, *_darwin.go, *_linux.go
internal/host/            the only package that imports Wails
frontend/                 React UI
build/                    Wails build tasks and per-OS assets
```

Rules:

- **`internal/core`, `internal/api`, `internal/engine` and `cmd/dtt` must not depend on Wails.** The headless dependency test checks core, API, engine and every existing command; CI runs them with `CGO_ENABLED=0` and no GTK. Only the host and app entry point import Wails.
- **Business logic does not go in React.** The UI renders state and sends intents; argv and checks come from the core, which keeps the preview-equals-execution guarantee in [architecture.md](architecture.md).
- **Daemon/API implemented in phase 16; GUI migration pending.** The diagram and layout above show today's code. [design/control-plane.md](design/control-plane.md) describes the local API served by `internal/control`, its `internal/client`, and the `cmd/dtt` lifecycle entry point in `internal/daemon`. Phase 14 has extracted the in-process engine and shared types; host subscriptions translate engine events into the existing Wails events. `dtt` must build with `CGO_ENABLED=0` and no Wails. Tunnels still stop when the app quits, by product definition.

```go
type Process interface {
	Start(ctx context.Context, spec SpawnSpec) (Handle, error) // new process group or Job Object
	Stop(h Handle, grace time.Duration) error                  // whole tree: graceful where possible, then force
}

type Secrets interface {
	Get(name string) (string, error) // ErrNotFound → Needs token; ErrUnavailable → keyring missing
	Set(name, value string) error
	Delete(name string) error
}

type UserAuth interface {
	Available() bool // false on desktops with no suitable prompt
	Confirm(ctx context.Context, reason string) error
}
```

## Platform differences

### Processes

| Concern | macOS | Linux | Windows |
|---|---|---|---|
| Group the tree | `setpgid` | `setpgid` | Job Object |
| Graceful stop | SIGTERM to group | SIGTERM to group | None for a hidden GUI child; v1 uses `TerminateJobObject` |
| Force stop | SIGKILL to group | SIGKILL to group | `TerminateJobObject` |
| App crash | Children survive; reconcile on next launch | `PR_SET_PDEATHSIG` plus reconcile | `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` |
| Console window | n/a | n/a | `CREATE_NO_WINDOW`, or a window flashes per tunnel |
| Command tunnels | `$SHELL -c` | `$SHELL -c` | `cmd /c` or PowerShell, chosen explicitly |

The current "SIGTERM, then SIGKILL" text in [architecture.md](architecture.md) holds for Unix only.

Add to the supervisor:

- **Crash recovery:** record PID and process start time in the state file. On launch, a record that still matches is an orphan, and the app offers to stop it. Start time protects against PID reuse.
- **Wake from sleep:** reset backoff and re-check readiness. Wails exposes wake events, though UniClipboard's audit covered these on beta.28 only.
- **Connection sharing:** pass `-o ControlMaster=no -o ControlPath=none` by default. Otherwise a forward can attach to an existing master that the supervisor does not own, and [ControlMaster is reported as unsupported on Windows](https://github.com/PowerShell/Win32-OpenSSH/issues/405) anyway.

### Finding and launching binaries

These decide whether the app works at all on first launch:

- **PATH:** apps opened from Finder or a desktop launcher do not inherit the terminal's PATH, so Homebrew's `ngrok` or `cloudflared` would appear missing. Resolve in this order: per-tool path set in settings, PATH from a login shell (`$SHELL -ilc`, with a timeout), known directories, then process PATH. On Windows, also check `%SystemRoot%\System32\OpenSSH`. Show the resolved path in the editor's checks.
- **Environment:** spawn from a clean copy with app-bundle variables removed (`LD_LIBRARY_PATH`, `GTK_*`, `GIO_*` and so on), or host binaries may load the wrong libraries.
- **Quoting:** the command preview and paste import need POSIX rules on Unix and Windows rules on Windows, tested separately.

The daemon inherits `SSH_AUTH_SOCK` when launched. A macOS Finder probe inherited the graphical launchd agent; a private localhost SSH session left its detached child with a stale forwarded-agent path after disconnect. Restart from the intended local/desktop session to change it. Detached Keychain reads remain unverified because the current library cannot select an isolated test keychain; no real user credentials were used.

### Askpass

The current design has the helper read the keychain and return a passphrase. Change it for two reasons:

1. **`SSH_ASKPASS_REQUIRE=force` sends every prompt to the helper**, potentially including host-key confirmation and OTP, not only key passphrases. Answering all of them with a stored passphrase would be wrong, and could answer a security question blindly.
2. Keychain access from a second binary can cause separate OS permission prompts.

Flow: the app listens on one local Unix socket in a private temp directory (Go supports `AF_UNIX` on Windows 10 1803 and later) and passes its address and a per-spawn token through the environment. `devt-tunnel-askpass` sends the prompt there and prints the answer. The app matches a passphrase prompt to the Vault entry and shows anything else in a dialog. The passphrase still never appears in argv or env.

Windows caveat: `SSH_ASKPASS` behavior depends on version. A [Win32-OpenSSH report](https://github.com/PowerShell/Win32-OpenSSH/issues/2115) shows it being ignored on 8.6 until `SSH_ASKPASS_REQUIRE` was set, while 8.1 behaved differently. Set `SSH_ASKPASS_REQUIRE=force` and a dummy `DISPLAY`, then test both versions.

### Secrets and "reveal"

| | macOS | Windows | Linux |
|---|---|---|---|
| Store | Keychain | Credential Manager (2,560-byte blob limit) | Secret Service over D-Bus |
| Store missing | Unlikely | Unlikely | Common on minimal or tiling setups |
| OS authentication for reveal | LocalAuthentication | Windows Hello or credential prompt | **No uniform option** |

- When Secret Service is unavailable, show a new `Keyring unavailable` state and do not write plaintext.
- The product rule "reveal requires OS authentication" cannot hold uniformly on Linux. Proposal: where `UserAuth.Available()` is false, secrets cannot be revealed, only replaced. A polkit prompt could be tried later.

### Desktop integration

| Feature | macOS | Windows | Linux X11 | Linux Wayland |
|---|---|---|---|---|
| Global shortcut | Carbon | `RegisterHotKey` | `XGrabKey` | Portal; compositor or user decides the binding |
| Tray | Menu bar | Notification area | StatusNotifierItem | StatusNotifierItem; GNOME usually needs an extension |
| Quick launch placement | Centered panel | Centered | Centered | Compositor decides |

Portal support is uneven: sources list KDE, Hyprland and recent GNOME as supported, and others as missing, and they disagree on details. Test on real desktops.

Product changes these imply:

- **Default shortcut per OS.** ⌥Space maps to Alt+Space, which opens the window menu on Windows and many Linux desktops. Propose `Ctrl+Alt+Space` there, with registration conflicts surfaced in settings. Elsewhere, ⌘ maps to Ctrl.
- **Tray switches become check items.** Native tray menus are the only cross-platform form, particularly on Linux, so "on/off switches" should read as checkmarks.
- **Tray is optional.** If no tray host exists, closing the main window must not leave the app invisible and still running tunnels. Likewise, if no global shortcut can be registered, quick launch stays reachable through ⌘K/Ctrl+K in the main window.

### Paths

| | macOS | Linux | Windows |
|---|---|---|---|
| Definitions | `~/.config/devt-tunnel/tunnels.toml` | `$XDG_CONFIG_HOME/devt-tunnel/tunnels.toml` | `%APPDATA%\devt-tunnel\tunnels.toml` |
| Runtime state | `~/Library/Application Support/devt-tunnel/` | `$XDG_STATE_HOME/devt-tunnel/` | `%LOCALAPPDATA%\devt-tunnel\` |

macOS keeps `~/.config` so the file stays with dotfiles. Shared definitions can contain OS-specific paths and commands, which belongs to the existing open question on import/export.

## Distribution

| | First | Later | Excluded |
|---|---|---|---|
| macOS | Signed, notarized `.dmg`; Homebrew cask | In-app update | Mac App Store: its sandbox conflicts with spawning arbitrary host binaries |
| Windows | Signed NSIS installer | WinGet, Scoop, portable zip | |
| Linux | deb, rpm | AppImage, AUR | Flatpak and strict Snap: tunnels need host binaries, and `flatpak-spawn --host` removes most of the sandbox's value |

- **CI:** build natively on macOS (arm64, x64), Windows and Ubuntu 24.04. Do not rely on Wails's experimental CGO-free Linux and macOS backends.
- **Linux baseline:** GTK ≥ 4.14 and WebKitGTK 6.0. UniClipboard documents that this drops native packages for Ubuntu 22.04 and Debian 12; the same trade-off is accepted here.
- **Windows WebView2:** preinstalled on Windows 11; the installer should bootstrap it for Windows 10.
- **Signing costs:** an Apple Developer membership and a Windows code-signing option. Without them, Gatekeeper and SmartScreen warnings will hurt first-run experience. Early builds can be unsigned for testers only.
- **Updates:** start with package managers and a "new version" notice. Add in-place updates with signature checks afterward; UniClipboard's signed manifest design is a useful reference.

## Validation spike

Build one minimal app on the pinned Wails version: tray, global shortcut, quick launch window, one supervised `ssh -N -L` tunnel, one askpass prompt and one keychain secret. Timebox: about 3 days. Test on real desktops, since a container with Xvfb does not count.

| # | Check | macOS | Win 11 | Ubuntu 24.04 GNOME Wayland | KDE Wayland | X11 |
|---|---|---|---|---|---|---|
| 1 | Global shortcut fires when unfocused; conflict is reported | ☐ | ☐ | ☐ | ☐ | ☐ |
| 2 | Quick launch appears focused, and hides on blur | ☐ | ☐ | ☐ | ☐ | ☐ |
| 3 | Tray menu updates while tunnels change state | ☐ | ☐ | ☐ | ☐ | ☐ |
| 4 | Stop leaves no process behind, including a tree from a Command tunnel | ☐ | ☐ | ☐ | | |
| 5 | Force-kill the app, then relaunch: orphan handled | ☐ | ☐ | ☐ | | |
| 6 | Askpass handles passphrase, host-key and OTP prompts | ☐ | ☐ | ☐ | | |
| 7 | App opened from launcher finds Homebrew or user-installed binaries | ☐ | ☐ | ☐ | | |
| 8 | Secret round trip; missing keyring handled | ☐ | ☐ | ☐ | | |
| 9 | No console window flashes when spawning | | ☐ | | | |
| 10 | Idle memory and installer size recorded | ☐ | ☐ | ☐ | | |

Extended checks still requiring real desktops: wake event delivery, OS-authenticated reveal, keyboard/IME behavior, multi-monitor palette focus, window restoration after monitor removal, Windows quoting, 500-tunnel rendering, second-instance focus, GNOME close behavior and installed helper discovery. Automated CI and cross-compilation are not acceptance evidence for these checks.

**Results so far (macOS arm64 only, Windows and Linux not run):**

| Check | Result |
|---|---|
| 4 | Passed with real `ssh` and a process tree (`internal/e2e`, `internal/platform`) |
| 5 | Passed: after `kill -9` the record survives and the UI offers to stop the orphan. An orphan's output pipe is gone, so it can be half broken; a Python server stopped answering. |
| 6 | Passed for host-key and passphrase prompts through the real dialog; OTP not run |
| 1, 2, 3, 7, 8 | Not run: they need a person at the desktop or a real keychain entry |
| 10 | Binary is 9.4 MB plus a 4.6 MB helper; idle memory and installer size not recorded |

The UI checks ran on a Wails server-mode build in a browser, not in the native window. The native app was launched and quit, but its windows were not inspected.

**Decision rule:**

- Checks 4–9 fail for reasons in our own code: fix them; the stack is unaffected.
- A check in 1–3 fails in Wails on macOS, Windows or X11 and has no small fix: repeat that check on Tauri 2 before switching.
- Wayland failures alone do not block, provided the fallbacks above work, because the platform limits any framework there.

## Risks

| Risk | Mitigation |
|---|---|
| Wails v3 stable line is new and changes quickly | Pin one version; upgrade deliberately; keep Wails inside `internal/host` |
| WebKitGTK rendering or GPU problems on Linux | Keep UI simple, avoid heavy effects; test on Nvidia; Electron remains a shell fallback for the same React UI |
| Three-OS support exceeds a small team | Release in order: macOS, Windows, then Linux, with the seams in place from day one |
| Differences across OpenSSH versions | Read `ssh -V`; keep flags conservative; test Windows inbox versions |
| Tauri would have been fine | Accepted; moving costs a rewrite of the host and core but not the UI |

## Sources

- UniClipboard: [repository](https://github.com/UniClipboard/UniClipboard), [Tauri retirement record](https://github.com/UniClipboard/UniClipboard/blob/main/docs/architecture/gui-go-tauri-retirement.md), [GUI host README and capability audit](https://github.com/UniClipboard/UniClipboard/blob/main/apps/gui-go/README.md), [Linux GTK4 migration](https://github.com/UniClipboard/UniClipboard/blob/main/docs/architecture/gui-go-linux-gtk4-migration.md)
- Wails: [v3.0.0](https://github.com/wailsapp/wails/releases/tag/v3.0.0), [v3.1.0](https://github.com/wailsapp/wails/releases/tag/v3.1.0), [CGO-free Linux backend](https://github.com/wailsapp/wails/pull/5715)
- Tauri: [global shortcut plugin](https://v2.tauri.app/plugin/global-shortcut/), [sidecars](https://v2.tauri.app/develop/sidecar/)
- Electron footprint and general comparison (blog, unverified): [digitalapplied.com](https://www.digitalapplied.com/blog/desktop-apps-web-stack-tauri-electron-deno-wails-2026)
- OS and tools: [Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), [ssh(1)](https://man.openbsd.org/ssh.1), [flatpak-spawn(1)](https://www.man7.org/linux/man-pages/man1/flatpak-spawn.1.html)
- Global shortcut portal support: [xfwl4 issue](https://gitlab.xfce.org/xfce/xfwl4/-/issues/93), [global-shortcut-portal](https://pypi.org/project/global-shortcut-portal/0.1.1/), [2024 overview, partly outdated](https://dec05eba.com/2024/03/29/wayland-global-hotkeys-shortcut-is-mostly-useless/)
- Secrets: [zalando/go-keyring](https://github.com/zalando/go-keyring)

From general knowledge, not checked against a source in this research: GNOME's tray extension requirement, the Mac App Store sandbox limit, the login-shell PATH behavior, WebView2 availability, the Credential Manager size limit, and the Alt+Space conflicts. The spike covers most of these.
