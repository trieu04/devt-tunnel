# Phase 21: Packaging `dtt`

**Goal:** every installer ships a signed `dtt` that the app can find, and that people can run from a terminal.

**Depends on:** 18, 20. Combines with [phase 13](13-distribution.md), and needs the same signing infrastructure.

## 21.1 Ship the binary

Follow every place `devt-tunnel-askpass` appears today:

| File | Change |
|---|---|
| `build/darwin/Taskfile.yml` | Copy into `Contents/MacOS`, and ad-hoc sign before the bundle |
| `build/linux/nfpm/nfpm.yaml` | Install `/usr/bin/dtt` |
| `build/windows/nsis/wails_tools.nsh` | Add to `wails.files`. Confirm the uninstaller removes it, and that it stops a running daemon first, or the file stays locked |
| `build/windows/Taskfile.yml` | Add to the sign step |
| `.github/workflows/release.yml` | Sign on macOS and Windows, in inner-to-outer order on macOS |
| `.github/workflows/ci.yml` | Add `./cmd/dtt` checks, and a `CGO_ENABLED=0` cross-build |

Also add a standalone archive per OS and architecture, containing `dtt` and `devt-tunnel-askpass`, for machines with no desktop. The daemon needs both.

## 21.2 Put it on PATH

- **macOS:** Settings → **Install command-line tool** makes a symlink in `/usr/local/bin`, after asking, or prints the command if that needs admin rights. Homebrew: the cask links it.
- **Windows:** add the install directory to the user's `PATH` as an installer option.
- **Linux:** already on `PATH` from the package.

## 21.3 Start at login, optional

`dtt daemon install [--start-marked]` and `uninstall`: a launchd agent on macOS, a systemd user unit on Linux. Windows comes later. Tests check the generated files. Starting them is manual acceptance.

## 21.4 Upgrades

Replacing binaries while a daemon runs:

- Same API: the old daemon keeps running until it exits. Document this in the release notes.
- Different API: the client behavior from [phase 16](16-daemon-api.md#164-internalclient) applies.

Test an upgrade with an active tunnel on each OS.

## 21.5 macOS checks

A second executable in the bundle, launched detached, must pass hardened runtime and notarization. Check that the keychain does not prompt on every call from the signed daemon. This needs owner infrastructure, see the plans README.

## Exit test

On a clean machine per OS: install, run `dtt version` from a new terminal, open the app, confirm it uses the bundled daemon, then uninstall and confirm nothing is left running.
