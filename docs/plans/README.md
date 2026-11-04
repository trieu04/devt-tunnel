# Implementation plans

How to take devt-tunnel from the current code base (commit `f4134ce`) to the product described in [product.md](../product.md), [architecture.md](../architecture.md) and [platform.md](../platform.md).

Baseline when this was written (2026-11-03): `go test ./...` passes on macOS arm64. Windows and Linux compile in CI but have never been run by a person.

## Phases

| # | Plan | Delivers | Needs | Estimate |
|---|---|---|---|---|
| 1 | [Core fixes](01-core-fixes.md) | Bugs found while reading the code, Windows quoting | | 3 d |
| 2 | [Persistence](02-persistence.md) | Safe `tunnels.toml` writes, state and settings files, model additions, draft API | 1 | 4 d |
| 3 | [SSH discovery](03-ssh-discovery.md) | Host list, resolved values, tool info, complete checks | 1 | 3 d |
| 4 | [Paste-command import](04-importer.md) | `ssh`, `ngrok`, `cloudflared` argv into the model | 1, 2 | 3 d |
| 5 | [Frontend shell](05-frontend-shell.md) | Screen navigation, design tokens, UI test setup, main window parity | 2, 3 | 4 d |
| 6 | [Tunnel editor](06-editor.md) | Create, edit, duplicate, delete | 2, 3, 4, 5 | 6 d |
| 7 | [Quick launch](07-quick-launch.md) | Grouped results, scopes, recents, all shortcuts | 3, 5, 6 | 3 d |
| 8 | [Profiles](08-profiles.md) | Active profile, activation behavior, screen, tray | 2, 5 | 4 d |
| 9 | [Vault](09-vault.md) | Secret index, Vault screen, reveal, start retry | 1, 2, 5 | 5 d |
| 10 | [Settings](10-settings.md) | Binaries, SSH and config files, shortcut, general | 2, 3, 5 | 3 d |
| 11 | [Reliability](11-reliability.md) | Wake from sleep, reconnect edge cases, log streaming | 1 | 3 d |
| 12 | [Cross-platform validation](12-cross-platform.md) | Validation matrix on real Windows and Linux desktops | 6–11 | 8 d |
| 13 | [Distribution](13-distribution.md) | Signed installers, release pipeline, update notice | 12 | 6 d |

Estimates are rough, for one developer, and have not been checked against real progress. Phase 12 is the least certain, because its cost depends on what breaks.

```text
1 ─┬─► 2 ─┬─► 4 ────────┐
   │      │             ├─► 6 ─► 7 ─┐
   ├─► 3 ─┴─► 5 ────────┤           │
   │          └─► 8, 9, 10 ─────────┼─► 12 ─► 13
   └─► 11 ──────────────────────────┘
```

Can run in parallel without touching the same files: 2 and 3, then 4 and 5, then 8, 9 and 10 once 5 is merged, and 11 at any point after 1. Phases 8, 9 and 10 each add methods to `internal/host/service.go`, so split that file in phase 2 (see its task 2.7) before they start. Phase 3 runs alongside that split, so it puts its methods in a new `service_ssh.go`, and each later phase does the same with its own file.

## Milestones

| Milestone | Phases | Exit test |
|---|---|---|
| **v0.2, editor** | 1–6 | A new user creates a working tunnel from an SSH host and from a pasted command without opening `tunnels.toml` |
| **v0.3, macOS feature complete** | 7–11 | Every row of the coverage table below works on macOS in the native window, not only in server mode |
| **v0.4, Windows** | 12 (Windows) | Windows column of the validation matrix is filled in |
| **v0.5, Linux** | 12 (Linux) | Linux columns are filled in; Wayland failures have working fallbacks |
| **v1.0** | 13 | Signed installers published by the release workflow; a clean machine installs and runs one tunnel |

The order macOS, Windows, Linux comes from the risk table in [platform.md](../platform.md).

## Decisions these plans assume

Each is a default chosen so that planning could continue. Changing one affects the phases listed.

| # | Decision | Why | Phases |
|---|---|---|---|
| D1 | Saving from the app rewrites `tunnels.toml`, so hand-written comments are lost. The previous file is kept as `tunnels.toml.bak`. | `BurntSushi/toml` cannot keep comments. A comment-preserving editor costs far more than this is worth. | 2 |
| D2 | Per-machine preferences go in a new `settings.toml` next to `tunnels.toml`. | `tunnels.toml` must stay shareable, and tool paths and shortcuts are machine-specific. | 2, 10 |
| D3 | The Vault shows a fixed mask, without the last four characters the mockup shows. | Showing them means reading every value from the keychain or storing part of a secret on disk. | 9 |
| D4 | Cut from the mockup: "Open in terminal", the tray's "Filter tunnels", and scheme-prefixed addresses such as `postgres://`. | None is in product.md. Each needs per-OS work or a guess about the service. | 6, 7, 8 |
| D5 | The mockup's "None, use ssh-agent" and "Ask every time" become one option. | The behavior is identical: without a stored secret, every prompt goes to the dialog. | 6 |
| D6 | One active profile at a time. This closes that open question in architecture.md. | Already listed under its Decisions. | 8 |
| D7 | A tunnel's SSH config is the active profile's if the tunnel belongs to it, otherwise that of its first profile that sets one. | Today only the second half exists (`optionsLocked`), so activating a profile would not change which config a tunnel uses. | 8 |
| D8 | ⌘K in the main window opens the quick launch palette. The top search box filters the list and loses its ⌘K hint. | product.md says ⌘K is quick launch. It is also the fallback when no global shortcut can be registered. | 7 |
| D9 | No import/export format in v1. Copying `tunnels.toml` is the sharing story. | Keeps an open question out of scope. Revisit after Windows shows how much paths differ. | |
| D10 | Tunnel ids are generated once from the name and never change. | Profiles, state and process records refer to them. | 2, 6 |

## Needs the project owner

These cannot be done from the code base:

- Apple Developer membership and a Windows code-signing option (phase 13).
- A license for the project. `build/linux/nfpm/nfpm.yaml` says `UNLICENSED`, and there is no `LICENSE` file at the root (phase 13).
- Real Windows and Linux machines, and a person at them, for the validation matrix (phase 12). A container with Xvfb does not count, per platform.md.
- Accounts for real `ngrok` and `cloudflared` runs, so that readiness patterns can be confirmed (phase 11).

## Conventions for every phase

- **Boundaries from platform.md hold.** `internal/core` has no Wails and no OS-specific code. Only `internal/host` and `main.go` import Wails. Argv, checks and state come from Go, and React renders them.
- **After changing a bound Go type or method**, regenerate bindings with `wails3 task common:generate:bindings` and commit `frontend/bindings`.
- **Tests:** run `go test ./internal/... ./cmd/...` and `pnpm --dir frontend test`. New core code uses the existing fakes in `supervisor_test.go` and `secret.Memory`. Core tests must also pass with `CGO_ENABLED=0`, which CI enforces.
- **Evidence:** a check is done only when run where it claims to be. Say which of unit test, `internal/e2e`, server-mode browser or native window was used.
- **Docs:** each phase updates the Status table in the root README, and corrects product, architecture or platform docs where it changes behavior.
- **Size:** one pull request per numbered task group where practical. Keep to the existing style: small packages, comments only where the reason is not obvious.

## Coverage of the product docs

| Requirement | Phase |
|---|---|
| Tunnel types and states | Done; fixes in 1, 11 |
| Launch flow: search, Enter, copy, log | 5, 7 |
| Missing secret → Vault → start retries | 9 |
| No search result offers "create from host" | 5, 7 |
| Create and edit: New, Edit, Duplicate; three sources; Save, Save & start | 6 |
| Editor checks: binary, host exists, port conflict, `-N`, secret | 3, 6 |
| Main screen: filters, profile selector, nested forwards, detail panel | 5, 8 |
| Quick launch: groups, recents | 7 |
| Tray: check items, optional, profile filter | Done; filter in 8 |
| Editor screen | 6 |
| Profiles screen and activation | 8 |
| Vault screen, reveal with OS authentication | 9 |
| All shortcuts | 5, 6, 7 |
| Host list and resolved values | 3 |
| Per-tool paths, resolved path in checks | 3, 10 |
| Paste-command import | 4 |
| Runtime state file: active profile, window state | 2 |
| Wake from sleep | 11 |
| POSIX and Windows quoting | 1 |
| Validation spike | 12 |
| Distribution | 13 |
