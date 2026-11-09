# Roadmap

Open work only. The per-phase plans for finished work were removed; they remain in git history before this file was added (`git log -- docs/plans`).

A check is done only when it has run where it claims to: say which of unit test, `internal/e2e`, server-mode browser or native window was used. CI and cross-compilation are not desktop acceptance.

## Milestones

| Milestone | Exit test |
|---|---|
| **Agents** | A real MCP client with no shell opens, uses and closes a tunnel, and a public tunnel waits for a person |
| **Windows** | Windows column of the [validation matrix](platform.md#validation) is filled in |
| **Linux** | Linux columns are filled in; Wayland failures have working fallbacks |
| **v1.0** | Signed installers published by the release workflow; a clean machine installs and runs one tunnel from the app and from `dtt` |

Order is macOS, Windows, Linux.

## Needs the project owner

- Apple Developer membership and a Windows code-signing option.
- Real Windows and Linux machines, and a person at them. A container with Xvfb does not count.
- Accounts for real `ngrok` and `cloudflared` runs, to confirm readiness patterns.

## Acceptance still open

- **Real MCP client:** record client versions and results in the [agent guide](cli/agents.md#client-compatibility-and-acceptance). The client opens a forward to an allow-listed alias, connects through it and closes it, leaving no process. Killing the client ends the lease after the grace period. A public tunnel is held until a person approves in the GUI.
- **Detached Keychain reads** from terminal and Finder, in an isolated macOS account or VM. `go-keyring` cannot select a disposable keychain.
- **Signed launch:** GUI-to-daemon start under hardened runtime and notarization; the keychain does not prompt on every call from the signed daemon.
- **Port warning** in the editor's native window.
- **OTP** through the askpass dialog, as an `internal/e2e` test if possible without root, otherwise manually.

## Cross-platform validation

Fill the [validation matrix](platform.md#validation) on real desktops, fix what fails, and apply the decision rule there.

| Machine | Purpose |
|---|---|
| macOS arm64, and x64 if available | Remaining matrix checks |
| Windows 11 | All checks, inbox OpenSSH |
| Windows 10 22H2 | WebView2 bootstrap, older inbox OpenSSH |
| Ubuntu 24.04 GNOME Wayland | Baseline Linux, no tray by default |
| Kubuntu or Fedora KDE Wayland | Tray and portal shortcut |
| Ubuntu 24.04 on X11 | `XGrabKey` path |
| A machine with an Nvidia GPU | WebKitGTK rendering |
| Minimal Linux with no Secret Service | `Keyring unavailable` |

### Windows

- **Job Object race:** `attach` assigns the job after `cmd.Start`, so a process that spawns children immediately can escape it. Start suspended, assign, then resume (`os/exec` does not expose the thread handle, so likely `NtResumeProcess`). Write the test first.
- **Silent fallback:** `attach` returns a bare handle if a job call fails, and then only the top process stops. Report it in the tunnel's log.
- **Daemon socket:** whether the per-user directory ACL is sufficient, and `AF_UNIX` with non-ASCII usernames and long paths.
- **Askpass** on both inbox OpenSSH versions, with the helper under `Program Files`.
- **Orphans:** `KILL_ON_JOB_CLOSE` should leave none; confirm the record is cleared as stale.
- **`cmd.exe`:** `&`, `%VAR%` and quotes in a Command tunnel; `configure` must stay in step with `platform.Shell`.
- **Binary lookup:** `loginShellPATH` is skipped, so check that `knownDirs` covers ngrok and Chocolatey installs.
- **Other:** Credential Manager size limit with long Cloudflare tokens; named cloudflared tunnels reconnect after a forced kill; `\r\n` in `logRing` and SSH configs; tray icon legibility on a dark taskbar.

### Linux

- **GNOME without a tray:** if the shortcut portal fails, add `devt-tunnel --quick` through `OnSecondInstanceLaunch` so a desktop shortcut can open quick launch.
- **Environment:** a deb launch passes a usable env and `SSH_AUTH_SOCK` to `ssh`. The same question applies to Finder launches on macOS.
- **Secret Service:** locked-collection behavior, and how long `Get` can block; `Cached` must not hold its lock during it.
- **Other:** `Pdeathsig` in a real desktop session; rendering on Nvidia and with fractional scaling; quick launch receives focus on Wayland.

### macOS

- With a hidden main window, clicking the dock icon reopens it.

### CI

- Try enabling `internal/e2e` on Windows runners, and record skip reasons.
- Smoke-test the built binary with a temporary config directory. Xvfb is fine for this and is not desktop acceptance.

## Distribution

Unsigned builds, the release workflow, deb/rpm contents, the update notice and bundled `dtt` exist. Remaining:

- **Windows:** sign `dtt.exe` in `build/windows/Taskfile.yml` and `release.yml` (only the app, helper and installer are signed). Confirm the installer bootstraps WebView2 on a clean Windows 10, and stops a running daemon before uninstalling. Confirm the `RMDir /r "$AppData\..."` in `project.nsi` cannot reach `tunnels.toml`. Exclude MSIX, and delete its template if unused.
- **macOS:** test notarization on a clean Mac with a real browser download; recheck Keychain prompts across an unsigned-to-signed upgrade. Publish a Homebrew cask from a personal tap first.
- **Linux:** confirm rpm dependency overrides on Fedora; install and remove scripts must not fail with no desktop database.
- **Upgrades:** test replacing binaries with an active tunnel on each OS.
- **README:** add a real screenshot of the app.
- **Later:** WinGet, Scoop, AppImage, AUR. In-place updates need an evaluation of Wails `pkg/updater` first: helper replacement, running tunnels, and package-manager-owned files.

## Later: headless servers

Not part of the milestones above. Run `dtt` on a headless server and manage it through its daemon socket, similar to `docker context`. The API has no remote transport today, so this needs its own design for reaching the socket and for authorization.

Groundwork when this starts:

- **Exit test:** on Linux with no GTK or display, one `dtt` binary adds and starts an SSH forward, answers a host-key prompt on the TTY, connects, reads logs and stops. Without Secret Service, secret-free tunnels work and tunnels with secrets report `keyring_unavailable`.
- **Standalone archives:** `build-cli.yml` packs only `dtt`, but the daemon also needs `devt-tunnel-askpass` for SSH prompts.
- **Login start:** `dtt daemon install [--start-marked]` and `uninstall`, using launchd and a systemd user unit; Windows later.
