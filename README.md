# devt-tunnel

Desktop app to define, find and run SSH, cloudflared, ngrok and custom-command tunnels from one place. It drives the binaries already installed on the machine and reuses `~/.ssh/config`.

Targets macOS, Windows and Linux. Built with Wails v3, Go and React.

## Status

Early code base. It has run on macOS only; Windows and Linux compile but have not been run.

| Area | State |
|---|---|
| Tunnel model, safe persistence, command builder and paste import | Implemented and unit tested |
| Supervisor: readiness, reconnect, wake recovery, stop and crash recovery | Implemented; real sleep/wake acceptance remains |
| SSH prompts through the askpass helper | Done |
| Secrets and Vault metadata | Implemented; reveal stays disabled until native OS authentication is available |
| Main window, editor, quick launch, profiles, Vault and Settings | Implemented; frontend tests cover only filtering, shortcuts and quick-launch results, and native visual acceptance remains |
| SSH host scan/`ssh -G`, tool discovery and per-tool paths | Implemented |
| Installers and release workflow | Unsigned builds automated; signing, notarization, license and clean-machine checks need owner infrastructure |
| Daemon, `dtt` CLI and MCP server for agents | Design accepted; not implemented |

Windows and Linux have compile/CI coverage but have not completed the real-desktop matrix in [docs/platform.md](docs/platform.md). Do not interpret cross-compilation as tray, shortcut, prompt or WebView acceptance.

## Install

Release artifacts are created as a draft from `v*` tags. Until signed artifacts are published, build locally with the development instructions below; files named `-unsigned` are tester-only.

- macOS: install the `.dmg` once the first signed and notarized release is published; Homebrew cask publication follows it.
- Windows 10/11: run the NSIS installer once signed releases exist. Windows 10 may install the WebView2 runtime during setup.
- Ubuntu 24.04+/Debian 13+: install the `.deb`; Fedora users install the `.rpm`. GNOME may need a StatusNotifierItem extension for a tray icon.

The app uses system binaries and does not bundle `ssh`, `ngrok` or `cloudflared`.

## First tunnel

Open **New tunnel**, choose **SSH**, select a host from the scanned SSH config, add a local forward such as `127.0.0.1:5433 → 127.0.0.1:5432`, review the generated command and checks, then choose **Save & start**. The same editor accepts a pasted `ssh`, `ngrok` or `cloudflared` command.

## Security and data

- `tunnels.toml` contains definitions and secret names, not secret values, and can be shared after reviewing hostnames, paths and custom commands.
- Secret values stay in macOS Keychain, Windows Credential Manager or Linux Secret Service. There is no plaintext fallback.
- Runtime metadata is in the OS state directory; per-machine paths and shortcuts are in `settings.toml` beside the definitions.
- Tunnel output stays in memory. The rotating application log excludes secret values.
- The optional release update check is the app's only network request of its own; tunnel processes make their normal connections.

## Development

Requires Go 1.27 or newer, Node 22, pnpm and the Wails CLI. On Linux, also GTK 4.14 or newer and WebKitGTK 6.0 (`libgtk-4-dev libwebkitgtk-6.0-dev` on Ubuntu 24.04).

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

```bash
wails3 dev
```

```bash
wails3 task build
```

```bash
go test ./internal/... ./cmd/...
```

```bash
pnpm --dir frontend test
```

`go test` needs no frontend build. `internal/e2e` starts a private `sshd` and is skipped where that is not possible. Building the root package needs `frontend/dist`, which `wails3 task build` produces.

## Layout

| Path | Purpose |
|---|---|
| `main.go` | Entry point |
| `internal/core` | Model, config, command builder, checks, supervisor; no GUI and no OS-specific code |
| `internal/askpass`, `cmd/devt-tunnel-askpass` | Forward ssh prompts to the app |
| `internal/platform` | Processes, environment, paths, keychain, per OS |
| `internal/host` | Wails services, windows, tray, shortcut; the only package using Wails besides `main.go` |
| `frontend` | React UI; `frontend/bindings` is generated |
| `build` | Wails build tasks and per-OS assets |

## Docs

- [docs/product.md](docs/product.md): concepts, tunnel types, flows, screens, shortcuts.
- [docs/architecture.md](docs/architecture.md): process model, SSH integration, secrets, storage, data model, decisions, open questions.
- [docs/platform.md](docs/platform.md): cross-platform stack, code layout, per-OS differences, distribution, validation checks.
- [docs/design/control-plane.md](docs/design/control-plane.md): central tunnel management, with [CLI](docs/design/cli.md) and [MCP](docs/design/mcp.md) contracts. Not implemented yet.
- [docs/plans/README.md](docs/plans/README.md): implementation plans.

## Design

UI and flow mockups: [docs/devTTunel UI.html](docs/devTTunel%20UI.html).
