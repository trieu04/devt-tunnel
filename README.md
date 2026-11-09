# devt-tunnel

Desktop app to define, find and run SSH, cloudflared, ngrok and custom-command tunnels from one place. It drives the binaries already installed on the machine and reuses `~/.ssh/config`.

Targets macOS, Windows and Linux. Built with Wails v3, Go and React.

## Status

Early code base. It has run on macOS only; Windows and Linux compile but have not been run.

| Area | State |
|---|---|
| Tunnel model, safe persistence, command builder and paste import | Implemented and unit tested |
| Supervisor: readiness, reconnect, wake recovery, stop and crash recovery | Implemented; native macOS sleep/wake acceptance passed |
| SSH prompts through the askpass helper | Done |
| Secrets and Vault metadata | Implemented; native macOS authentication and reveal passed after correcting the device-owner policy |
| Main window, editor, quick launch, profiles, Vault and Settings | Implemented; phase 17 native macOS acceptance passed, while Windows and Linux remain pending |
| SSH host scan/`ssh -G`, tool discovery and per-tool paths | Implemented |
| Installers and release workflow | Unsigned builds automated; signing, notarization and clean-machine checks need owner infrastructure |
| Shared API types and GUI-independent engine (phase 14) | Extracted and unit tested; the daemon now owns the engine; native-window regression check pending |
| Typed readiness errors, endpoint lists and live port checks (phase 15) | Implemented and unit tested; editor/native-window warning acceptance pending |
| Daemon, local API and `dtt daemon` lifecycle (phase 16) | Implemented with contract/process tests and real SSH forwarding; detached Keychain access and native cross-platform acceptance remain unverified |
| GUI as daemon client (phase 17) | Implemented with typed forwarding, event/reconnect tests and dependency guards; all native macOS checks passed |
| `dtt` CLI (phases 18–19) | Saved and ephemeral tunnels, leases, approvals, audit and generated/offline documentation implemented and tested; native/Linux no-GTK exit acceptance pending |
| Ownership, leases and policy (phase 19) | Daemon policy and approvals, CLI, agent badges, ephemeral Save and Settings → Agents implemented and tested; native macOS approval acceptance passed |
| MCP server and agent guide (phase 20) | Implemented with automated checks; real-client check pending |

Windows and Linux have compile/CI coverage but have not completed the real-desktop matrix in [docs/platform.md](docs/platform.md). Do not interpret cross-compilation as tray, shortcut, prompt or WebView acceptance.

## Install

Release artifacts are created as a draft from `v*` tags. Until signed artifacts are published, build locally with the development instructions below; files named `-unsigned` are tester-only.

- macOS: install the `.dmg` once the first signed and notarized release is published; Homebrew cask publication follows it.
- Windows 10/11: run the NSIS installer once signed releases exist. Windows 10 may install the WebView2 runtime during setup.
- Ubuntu 24.04+/Debian 13+: install the `.deb`; Fedora users install the `.rpm`. GNOME may need a StatusNotifierItem extension for a tray icon.

The app uses system binaries and does not bundle `ssh`, `ngrok` or `cloudflared`.

## `dtt`: headless install and usage

The CLI needs only Go 1.27.3 or newer to build, not Node, GTK or a display. From this checkout:

```bash
CGO_ENABLED=0 go build -o bin/dtt ./cmd/dtt
mkdir -p "$HOME/.local/bin"
install -m 755 bin/dtt "$HOME/.local/bin/dtt"
```

Add `$HOME/.local/bin` to `PATH`. On Windows, use `go build -o bin/dtt.exe ./cmd/dtt` and put the binary in a directory on `PATH`. Signed CLI packages and installer integration are planned for phase 21.

```bash
dtt add ssh db --host server1 -L 5433:127.0.0.1:5432
dtt start db
dtt ls
dtt show db --json
dtt addr db --port
dtt logs db -n 20
dtt stop db
dtt completion zsh > _dtt
dtt docs
```

`--host` reuses `~/.ssh/config`. The daemon starts on demand and shares saved tunnels with the GUI. `--json` provides versioned script output; `--no-input` prevents prompts. `dtt version` never starts a daemon, and `--no-daemon-start` limits other commands to an existing daemon. Shell completion never starts one either. `dtt daemon run|status|stop|restart` controls the daemon; `run --idle-exit 0` disables its default five-minute idle exit. `stop` and `restart` stop all daemon-owned tunnels; quitting the GUI does the same.

Read the [quick start](docs/cli/quickstart.md), [traditional-command replacements](docs/cli/replacing.md), [scripts](docs/cli/scripts.md), [agent guide](docs/cli/agents.md), [troubleshooting and exit codes](docs/cli/troubleshooting.md), or the [generated command reference](docs/cli/reference/dtt.md). The five guides are also available offline through `dtt docs <topic>`. `dtt man --dir ./man` writes a full section 1 manual. `go run ./internal/cli/gen` refreshes the committed reference and generates package-time man pages under ignored `bin/man/`.

One-off `run`, `forward`, `expose` and `save` commands, leases, approvals and audit are available. Background tunnels use detached person leases: CLI exit does not stop them, and no expiry applies unless `--ttl` is set. Use `dtt stop` or `dtt leases revoke` to close them. `expose` requires `--via` and always warns that the service will be public. `dtt mcp config` prints client setup using this executable's absolute path; `dtt mcp serve` exposes policy-controlled tools over stdio. `mcp config --json` is paste-ready client configuration and `mcp tools --json` is the raw tool-definition array, deliberately without the usual CLI envelope. See the [agent guide](docs/cli/agents.md) for approvals, lease cleanup and the pending real-client compatibility checks. Daemon login installation is not implemented yet. Native/Linux no-GTK CLI exit acceptance has not yet been run.

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

The headless CLI has its own [install and usage instructions](#dtt-headless-install-and-usage), [interface contract](docs/design/cli.md) and [daemon API](docs/design/control-plane.md#local-api). Its documentation package needs no frontend build either.

## Layout

| Path | Purpose |
|---|---|
| `main.go` | Entry point |
| `cmd/dtt`, `internal/daemon`, `internal/control`, `internal/client` | Headless daemon, local JSON-RPC server and client |
| `internal/api`, `internal/engine` | Shared contracts and tunnel orchestration |
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
- [CLI quick start](docs/cli/quickstart.md), [replacing traditional commands](docs/cli/replacing.md), [scripts](docs/cli/scripts.md), [agent guide](docs/cli/agents.md), [troubleshooting](docs/cli/troubleshooting.md) and [command reference](docs/cli/reference/dtt.md): also available with `dtt docs` / `dtt help`.
- [docs/design/control-plane.md](docs/design/control-plane.md): central tunnel management, with [CLI](docs/design/cli.md) and [MCP](docs/design/mcp.md) contracts. Daemon/API, GUI migration and phases 18–20 CLI/MCP commands are implemented with automated checks; the MCP real-client check remains pending.
- [docs/plans/README.md](docs/plans/README.md): implementation plans.

## License

Licensed under the [Apache License, Version 2.0](LICENSE). Bundled fonts keep their own licenses (SIL OFL) in `frontend/public`.
