# devt-tunnel

Desktop app to define, find and run SSH, cloudflared, ngrok and custom-command tunnels from one place. It drives the binaries already installed on the machine and reuses `~/.ssh/config`.

Targets macOS, Windows and Linux. Built with Wails v3, Go and React.

## Status

Early code base. It has run on macOS only; Windows and Linux compile but have not been run.

| Area | State |
|---|---|
| Tunnel model, `tunnels.toml`, command builder | Done |
| Supervisor: readiness, auto-reconnect, stop, crash recovery | Done |
| SSH prompts through the askpass helper | Done |
| Secrets in the OS keychain | Done; not yet exercised against a real keychain |
| Main window, quick launch, tray, global shortcut | First version; no tunnel editor yet |
| Editor, paste-command import, profile activation, Vault screen | Not started |
| `ssh -G` host list, per-tool path settings, reveal with OS authentication | Not started |
| Signed installers, updater | Not started |

Until the editor exists, tunnels are defined by editing `tunnels.toml`: use **Edit tunnels.toml** in the app, then **Reload**.

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

## Design

UI and flow mockups: https://claude.ai/artifact/38AsU71MWNsiUGp4FH4vs2 (private; ask the owner for access).
