# Phase 12: Cross-platform validation

**Goal:** fill the validation matrix in [platform.md](../platform.md) on real desktops, fix what fails, and decide about the stack using its decision rule.

**Depends on:** phases 6–11, so that real features are tested rather than a spike.

**Needs a person at each machine.** Automated runs cannot sign off tray, shortcut, focus or OS prompts, and platform.md rejects Xvfb as evidence.

## 12.1 Start early with the risky checks

Do not wait for phases 6–11 to run everything. As soon as phase 1 merges, run checks 1–3 on Windows 11 and on Ubuntu with the current build, because they alone can trigger a Tauri comparison under the decision rule. Learning this after the UI is built still leaves the UI intact, since the host is isolated, but it changes priorities.

## 12.2 Test machines

| Machine | Purpose |
|---|---|
| macOS arm64 (and x64 if available) | Remaining checks 1, 2, 3, 7, 8, 10 |
| Windows 11 | All, with inbox OpenSSH |
| Windows 10 22H2 | WebView2 bootstrap, and an older inbox OpenSSH |
| Ubuntu 24.04 GNOME Wayland | Baseline Linux, no tray by default |
| Kubuntu or Fedora KDE Wayland | Tray and portal shortcut |
| Ubuntu 24.04 on X11 | `XGrabKey` path |
| A machine or VM with an Nvidia GPU | WebKitGTK rendering risk |
| Minimal Linux with no Secret Service | `Keyring unavailable` |

## 12.3 Extended matrix

The ten checks in platform.md, plus these for features built since:

| # | Check |
|---|---|
| 11 | Wake from sleep: which event fires; a backoff restarts (phase 11) |
| 12 | Reveal: OS prompt appears, in front of the app; cancel handled; Linux shows no Reveal |
| 13 | Keyboard: ⌘ or Ctrl mapping; copy and paste in inputs; IME input in search |
| 14 | Quick launch on multi-monitor; focus returns to the previous app on hide |
| 15 | Window restores its size; never off-screen after a monitor is removed |
| 16 | Paste import and preview use the right dialect: try `C:\Program Files\…` paths on Windows |
| 17 | Dark and light rendering, fonts, and scrolling at 500 tunnels on each WebView |
| 18 | Two instances: second launch focuses the first |
| 19 | Tray without a host on GNOME: closing quits, after the quit confirmation |
| 20 | Helper is found from the installed location on each OS |

Record results in platform.md with OS version and date. Say which were seen in a native window.

Phase 17 adds these daemon-client checks; every cell requires a person in a native window:

| Check | macOS | Windows | Linux |
|---|---|---|---|
| First GUI launch starts or joins the daemon and shows tunnels | ☐ | ☐ | ☐ |
| Hidden-window prompt comes forward and accepts an answer | ☐ | ☐ | ☐ |
| App quit stops tunnels and exits the daemon | ☐ | ☐ | ☐ |
| GUI crash preserves tunnels; relaunch reconnects | ☐ | ☐ | ☐ |
| Daemon crash shows the banner; restart reconnects and offers orphans where supported | ☐ | ☐ | ☐ |
| Sleep and wake retries reconnecting tunnels promptly | ☐ | ☐ | ☐ |

## 12.4 Windows tasks

Known items from reading the code:

- **Job Object race.** `attach` assigns the job after `cmd.Start`, so a process that spawns children immediately can have them escape it. Start suspended with `CREATE_SUSPENDED`, assign, then resume. `os/exec` does not expose the thread handle, so `NtResumeProcess` from `ntdll` is the likely route. Write the test first: `cmd /c start`-style tree, Stop, and check that nothing survives (matrix check 4).
- **Silent fallback.** `attach` returns a bare handle if any job call fails, and then only the top process stops. Log it to the tunnel's log, as this would otherwise look like a leak with no explanation.
- **`SSH_ASKPASS` versions.** Test passphrase, host-key and OTP prompts on the inbox OpenSSH of both Windows versions, per the caveat in platform.md. The helper is run via `SSH_ASKPASS`, so check paths with spaces under `Program Files`.
- **AF_UNIX** in `os.MkdirTemp` on both versions, including a username with non-ASCII characters and a long path.
- **Identity.** Orphans: `KILL_ON_JOB_CLOSE` should mean no orphans after a crash. Verify, and confirm that the record is cleared as stale on next launch.
- **cmd.exe.** `configure` special-cases argument shape, and it must stay in step with `platform.Shell`. Test `&`, `%VAR%`, and quotes in a Command tunnel.
- **Console flash.** Check 9, for `ssh`, the helper and Command tunnels, including the login shell lookups. `loginShellPATH` is skipped on Windows, so only tools on `PATH` and `knownDirs` are found. Check that `%USERPROFILE%\.ngrok` style installs and Chocolatey's `C:\ProgramData\chocolatey\bin` are covered, and add them to `knownDirs` if not.
- **Keychain.** Credential Manager round trip and size limit. Long Cloudflare tokens are the likely test.
- **Reveal** with CredUI (phase 9).
- **Signal behavior.** Graceful stop does not exist here, so cloudflared and ngrok are killed. Confirm that a named tunnel reconnects cleanly on the next start.
- **Line endings.** `logRing` with `\r\n` output, and SSH config files with CRLF.
- **Tray icon.** `tray_other.go` assumes a tray exists. Confirm the light icon is readable on a dark taskbar, and switch by theme if not.

## 12.5 Linux tasks

- **GNOME without a tray.** `trayAvailable` returns false, and closing quits. Global shortcut portal on Ubuntu 24.04's GNOME version: expected to be uncertain. Fallback: ⌘K in the window, plus a suggestion to bind a desktop shortcut to a second launch, which `OnSecondInstanceLaunch` can map to opening quick launch. Add that if the portal fails: `devt-tunnel --quick`.
- **`Pdeathsig` result** from phase 1, confirmed on a real desktop session.
- **Secret Service** presence and locked collection behavior: does the OS prompt appear, and how long may `Get` block? `Cached` must not hold its lock during a blocking call, or the UI freezes.
- **Environment.** `CleanEnviron` only strips bundle variables when `APPDIR` is set. Confirm that a deb launch passes a usable env to `ssh`, and that `SSH_AUTH_SOCK` is there when launched from the desktop. This matters more than PATH, since a missing agent socket makes every key tunnel ask for a passphrase. The same applies to Finder launches on macOS, where the login-shell lookup only captures PATH. Decide whether to take `SSH_AUTH_SOCK` from the login shell too.
- **Rendering** on Nvidia and with fractional scaling.
- **Quick launch placement** on Wayland is decided by the compositor, per platform.md. Accept it, but make sure it receives focus. If focus fails on some compositor, this counts as a Wayland-only failure under the decision rule.

## 12.6 macOS tasks

- Remaining checks 1, 2, 3, 7, 8, 10. These are also the first to run.
- The dock: with `ApplicationShouldTerminateAfterLastWindowClosed: false` and a hidden main window, confirm that clicking the dock icon reopens it (`ApplicationShouldHandleReopen`-style handling may be needed).
- Consider `LSUIElement`: not in product.md. Keep the dock icon.

## 12.7 CI additions

- CI already builds on all three, but `go test` on Windows skips what needs `sshd`. Windows runners have OpenSSH server available, so try enabling `internal/e2e` there, and record skip reasons in the job log.
- Add a smoke test of the built binary: start with a temporary config directory, wait for a health line in the log, then quit. This needs an env override for the config path. Xvfb is fine for this smoke test, and it does not count as desktop acceptance.

## Decision point

Apply platform.md's decision rule, and record the outcome there:

- Fix what is in our own code.
- A check in 1–3 failing in Wails on macOS, Windows or X11 with no small fix: repeat that check on Tauri 2 before switching.
- Wayland failures alone do not block, if the fallbacks work.

## Done when

- The matrix is complete, with results or a documented fallback for each cell.
- platform.md's status line changes from "checks are still open".
- README states which platforms are supported at what level.
