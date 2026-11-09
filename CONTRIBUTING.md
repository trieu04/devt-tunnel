# Contributing

## Prerequisites

- Go 1.27.3 or newer, which is all `dtt` and the Go tests need
- Node 22 and pnpm
- The Wails CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`
- On Linux: GTK 4.14 or newer and WebKitGTK 6.0 (`libgtk-4-dev libwebkitgtk-6.0-dev` on Ubuntu 24.04)

## Develop and test

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

## Generated files

- After changing a bound Go type or method, run `wails3 task common:generate:bindings` and commit `frontend/bindings`.
- After changing CLI commands, run `go run ./internal/cli/gen` and commit `docs/cli/reference`. CI fails on drift.

## Project layout

| Path | Purpose |
| --- | --- |
| `main.go` | App entry point |
| `cmd/dtt`, `internal/daemon`, `internal/control`, `internal/client`, `internal/cli` | CLI, daemon, local JSON-RPC server and client |
| `internal/mcpserver` | MCP adapter |
| `internal/api`, `internal/engine` | Shared contracts and tunnel orchestration |
| `internal/core` | Model, config, command builder, checks, supervisor; no GUI and no OS-specific code |
| `internal/askpass`, `cmd/devt-tunnel-askpass` | Forward ssh prompts to the daemon |
| `internal/platform` | Processes, environment, paths, keychain, per OS |
| `internal/host` | Wails services, windows, tray, shortcut; the only package using Wails besides `main.go` |
| `frontend` | React UI; `frontend/bindings` is generated |
| `build` | Wails build tasks and per-OS assets |

The rules that keep this layout are in [docs/platform.md](docs/platform.md#rules).

## Pull requests

- Branch from `main` and open the pull request against it.
- Commit messages follow Conventional Commits, for example `feat(cli): ...` or `fix(ci): ...`.
- Keep `docs/` in step with behavior. Open work belongs in the [roadmap](docs/roadmap.md).
- Say where a check ran: unit test, `internal/e2e`, server-mode browser or native window. CI and cross-compilation are not desktop acceptance.
