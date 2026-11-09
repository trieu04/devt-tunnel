# Platform

Cross-platform stack, per-OS behavior and validation for macOS, Windows and Linux. The research behind these choices is in git history.

## Stack

| Layer | Choice |
|---|---|
| App shell | Wails v3.1.0, pinned (Go host, system WebView) |
| Core | Go packages with no GUI dependency |
| UI | React + TypeScript + Vite, built with pnpm |
| Process model | On-demand daemon with the GUI as a client ([design](design/control-plane.md)), plus a small askpass helper |
| Secrets | `zalando/go-keyring` behind an interface; never a plaintext fallback |
| Linux packages | deb and rpm first, AppImage later; no Flatpak or strict Snap |

Wails fits because most of devt-tunnel is supervising processes and integrating with the desktop, and Wails ships tray, global shortcut and windows first-party. Its risk is maturity, so the core stays independent of it. **Tauri 2 with a Rust core** is the fallback if validation fails; Electron remains a shell fallback for the same React UI if rendering fails. Three native apps were rejected as too costly, and Flutter or Compose would need third-party tray and shortcut plugins.

## Rules

- **`internal/core`, `internal/api`, `internal/engine` and `cmd/devt-tunnel` must not depend on Wails.** The headless dependency test checks this; CI builds them with `CGO_ENABLED=0` and no GTK. Only `internal/host` and `main.go` import Wails.
- **Business logic does not go in React.** The UI renders state and sends intents. Argv and checks come from the core, which keeps the preview-equals-execution guarantee in [architecture.md](architecture.md).

## Platform differences

### Processes

| Concern | macOS | Linux | Windows |
|---|---|---|---|
| Group the tree | `setpgid` | `setpgid` | Job Object |
| Graceful stop | SIGTERM to group | SIGTERM to group | None for a hidden GUI child |
| Force stop | SIGKILL to group | SIGKILL to group | `TerminateJobObject` |
| App crash | Children survive; reconcile on next launch | `PR_SET_PDEATHSIG` plus reconcile | `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` |
| Console window | n/a | n/a | `CREATE_NO_WINDOW`, or a window flashes per tunnel |
| Command tunnels | `$SHELL -c` | `$SHELL -c` | `cmd /c` or PowerShell, chosen explicitly |

Process records keep the start time as well as the PID, which protects orphan detection against PID reuse.

### Finding and launching binaries

- **PATH:** apps opened from Finder or a desktop launcher do not inherit the terminal's PATH. Resolve in this order: per-tool path from settings, PATH from a login shell (`$SHELL -ilc`, with a timeout), known directories, then process PATH. On Windows, also check `%SystemRoot%\System32\OpenSSH`. The editor's checks show the resolved path.
- **Environment:** spawn from a clean copy with app-bundle variables removed (`LD_LIBRARY_PATH`, `GTK_*`, `GIO_*` and so on), or host binaries may load the wrong libraries.
- **Quoting:** the command preview and paste import use POSIX rules on Unix and Windows rules on Windows.
- **SSH agent:** the daemon inherits `SSH_AUTH_SOCK` when launched and does not replace it. A session that disconnects leaves a stale path; restart the daemon from the intended session.

### Askpass

The flow is in [architecture.md](architecture.md#ssh-integration). Go supports `AF_UNIX` on Windows 10 1803 and later. On Windows, `SSH_ASKPASS` behavior depends on the OpenSSH version ([report](https://github.com/PowerShell/Win32-OpenSSH/issues/2115)). Set `SSH_ASKPASS_REQUIRE=force` and a dummy `DISPLAY`.

### Secrets

| | macOS | Windows | Linux |
|---|---|---|---|
| Store | Keychain | Credential Manager (2,560-byte blob limit) | Secret Service over D-Bus |
| Store missing | Unlikely | Unlikely | Common on minimal or tiling setups |
| OS authentication for reveal | LocalAuthentication | Windows Hello or credential prompt | None uniform: values can be replaced, not revealed |

### Desktop integration

| Feature | macOS | Windows | Linux X11 | Linux Wayland |
|---|---|---|---|---|
| Global shortcut | Carbon | `RegisterHotKey` | `XGrabKey` | Portal; compositor or user decides the binding |
| Tray | Menu bar | Notification area | StatusNotifierItem | StatusNotifierItem; GNOME usually needs an extension |
| Quick launch placement | Centered panel | Centered | Centered | Compositor decides |

Portal support is uneven across compositors. Without a tray host, closing the main window quits the app. Without a global shortcut, quick launch stays reachable through ⌘K/Ctrl+K.

### Paths

| | macOS | Linux | Windows |
|---|---|---|---|
| Definitions | `~/.config/devt-tunnel/tunnels.toml` | `$XDG_CONFIG_HOME/devt-tunnel/tunnels.toml` | `%APPDATA%\devt-tunnel\tunnels.toml` |
| Runtime state | `~/Library/Application Support/devt-tunnel/` | `$XDG_STATE_HOME/devt-tunnel/` | `%LOCALAPPDATA%\devt-tunnel\` |

## Distribution

| | First | Later | Excluded |
|---|---|---|---|
| macOS | Signed, notarized `.dmg`; Homebrew cask | In-app update | Mac App Store: its sandbox conflicts with spawning host binaries |
| Windows | Signed NSIS installer | WinGet, Scoop, portable zip | |
| Linux | deb, rpm | AppImage, AUR | Flatpak and strict Snap: tunnels need host binaries |

- **CI:** build natively on macOS (arm64, x64), Windows and Ubuntu 24.04. Do not rely on Wails's experimental CGO-free Linux and macOS backends.
- **Linux baseline:** GTK ≥ 4.14 and WebKitGTK 6.0, which drops native packages for Ubuntu 22.04 and Debian 12.
- **Windows WebView2:** preinstalled on Windows 11; the installer bootstraps it for Windows 10.
- **Updates:** a "new version" notice and package managers first; in-place updates with signature checks afterward.

## Validation

Test on real desktops. A container with Xvfb does not count.

| # | Check | macOS | Win 11 | Ubuntu 24.04 GNOME Wayland | KDE Wayland | X11 |
|---|---|---|---|---|---|---|
| 1 | Global shortcut fires when unfocused; conflict is reported | ☐ | ☐ | ☐ | ☐ | ☐ |
| 2 | Quick launch appears focused, and hides on blur | ☐ | ☐ | ☐ | ☐ | ☐ |
| 3 | Tray menu updates while tunnels change state | ☐ | ☐ | ☐ | ☐ | ☐ |
| 4 | Stop leaves no process behind, including a tree from a Command tunnel | ☑ | ☐ | ☐ | | |
| 5 | Force-kill the app, then relaunch: orphan handled | ☑ | ☐ | ☐ | | |
| 6 | Askpass handles passphrase, host-key and OTP prompts | ◐ | ☐ | ☐ | | |
| 7 | App opened from launcher finds Homebrew or user-installed binaries | ☐ | ☐ | ☐ | | |
| 8 | Secret round trip; missing keyring handled | ☐ | ☐ | ☐ | | |
| 9 | No console window flashes when spawning | | ☐ | | | |
| 10 | Idle memory and installer size recorded | ☐ | ☐ | ☐ | | |

Extended checks, on each OS: wake event delivery, OS-authenticated reveal, keyboard and IME input, multi-monitor quick launch focus, window restoration after monitor removal, Windows path quoting, 500-tunnel rendering in light and dark, second-instance focus, GNOME close behavior, installed helper discovery.

Daemon-client checks, in a native window on Windows and Linux: first launch starts or joins the daemon; a hidden-window prompt comes forward; quit stops tunnels and the daemon; a GUI crash preserves tunnels and relaunch reconnects; a daemon crash shows the banner and restart reconnects; sleep and wake retry reconnecting tunnels.

**macOS arm64 evidence so far** (Windows and Linux have not been run):

- Checks 4–5 passed with real `ssh` and process trees. After `kill -9`, an orphan's output pipe is gone, so it can be half broken.
- Check 6 passed for host-key and passphrase prompts; OTP not run.
- The GUI-as-daemon-client native checklist passed on 2026-11-05, including Vault reveal after correcting the device-owner authentication policy.
- Agent approval passed in the native window on 2026-11-05: a public `-R` request waited with no process started and offered no "always"; badge, ephemeral Save, lease revoke and disconnect grace behaved as designed.
- Native sleep and wake recovery passed.
- Binary is 9.4 MB plus a 4.6 MB helper; idle memory and installer size not recorded.

Remaining work is in the [roadmap](roadmap.md).

**Decision rule:**

- Checks 4–9 fail for reasons in our own code: fix them; the stack is unaffected.
- A check in 1–3 fails in Wails on macOS, Windows or X11 and has no small fix: repeat that check on Tauri 2 before switching.
- Wayland failures alone do not block, provided the fallbacks above work.

## Risks

| Risk | Mitigation |
|---|---|
| Wails v3 stable line is new and changes quickly | Pin one version; upgrade deliberately; keep Wails inside `internal/host` |
| WebKitGTK rendering or GPU problems on Linux | Keep the UI simple; test on Nvidia |
| Three-OS support exceeds a small team | Release in order: macOS, Windows, then Linux |
| Differences across OpenSSH versions | Read `ssh -V`; keep flags conservative; test Windows inbox versions |
