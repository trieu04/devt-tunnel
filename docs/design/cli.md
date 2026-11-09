# `devt-tunnel` command line

Status: implemented. Open acceptance is in the [roadmap](../roadmap.md). Architecture and rationale are in [control-plane.md](control-plane.md).

`devt-tunnel` complements the desktop app: people and agents use it to create managed tunnels in place of plain commands. This file is the contract for behavior a command listing cannot express. Commands and flags are in the [generated reference](../cli/reference/devt-tunnel.md); usage is in the [guides](../cli/quickstart.md).

## Concepts

- **Tunnel:** one supervised process. **Saved** tunnels are in `tunnels.toml`. **Ephemeral** tunnels, with ids starting `tmp-`, come from `run`, `forward` and `expose`, and are removed when they stop.
- **Daemon:** a small background process that owns all tunnels. Any command starts it when needed. It exits when idle. The desktop app uses the same daemon, so both show the same tunnels.
- **Referring to a tunnel:** by exact id, exact name or a unique id prefix. An ambiguous reference exits with code 2 and lists the matches.

## One-off tunnels

Port maps use the form `listen:target`:

| Form | Means |
|---|---|
| `5433:5432` | `127.0.0.1:5433` to port 5432 on the far side's loopback |
| `5433:db:5432` | `127.0.0.1:5433` to `db:5432` as seen from the SSH host |
| `0.0.0.0:5433:db:5432` | Listen on all interfaces. Prints a warning |
| `auto:5432` | Pick a free local port, and print it |

`devt-tunnel expose` requires `--via`: `internal/core/settings` does not yet implement `default_expose`. The two services differ in cost and in who can reach the URL. It always prints a stderr line saying the service will be public, including with `--quiet` or `--dry-run`. `--secret` names an existing keychain secret. For cloudflared, `--secret` selects a token-based named tunnel; `--domain` then describes an already-routed hostname and does not configure routing. Quick Cloudflare tunnels cannot choose a domain.

`--detach` and background forms return once the tunnel is ready, or exit with an error. They retain a detached person lease, not a pin, so CLI exit does not apply connection grace. Without `--ttl`, detached leases have no expiry: stop with `devt-tunnel stop` or `devt-tunnel leases revoke`. An explicit `--ttl` is positive and at most 8h, and stops the tunnel after that time. Foreground leases default to 30m; Ctrl-C closes the lease and stops the tunnel. Promoting with `--save` or `devt-tunnel save` preserves the process and lease, including its expiry.

`--dry-run` uses `tunnel.evaluateDraft` to print the exact command and findings without starting. Auto ports are allocated only by `tunnel.runEphemeral`, so a dry run with `auto` exits 2 with a concrete-port hint rather than inventing an address.

## Secrets

Values are read from a hidden prompt or from stdin, never from a flag, so they stay out of shell history and `ps`. There is no `reveal`: it requires OS authentication, which the desktop app provides.

## Approvals

`devt-tunnel approvals approve` needs a TTY and the code shown in the request.

## Global behavior

### Flags

`--json` · `-q, --quiet` · `--no-color` (also `NO_COLOR`) · `--timeout D` · `--no-daemon-start` · `--no-input`.

### Output

- Results go to stdout and diagnostics to stderr. Tables are for people and may change.
- `--json` is the stable interface. Result objects carry `apiVersion` (`api.Version`). New fields may appear, and removing or renaming a field raises the version.
- Deliberate exceptions: `devt-tunnel mcp config --json` and `devt-tunnel mcp tools --json` have no envelope, and `devt-tunnel mcp serve` writes only JSON-RPC to stdout.
- Colors, spinners and tables appear only on a TTY.
- `ls --json` and `show --json` include `owner` with `pinned`, `ephemeral` and `leases`; active claims also include a holder and expiry.
- `logs --json -f` emits one object per batch. Log lines have sequence numbers but no timestamps, so a positive `--since` exits 2.

With `--json`, errors are written to stderr as a single object:

```json
{ "apiVersion": 2, "error": { "code": "needs_secret", "message": "secret \"ngrok-personal\" is missing", "tunnel": "web", "hint": "devt-tunnel secret set ngrok-personal" } }
```

### Exit codes

Scripts can rely on these.

| Code | Name | Meaning |
|---|---|---|
| 0 | | Success |
| 1 | `failed` | The tunnel or operation failed, including a person revoking a lease (`lease_revoked`), a policy refusal (`policy_denied`) or disabled agent access (`agent_disabled`). See the message and `devt-tunnel logs db` |
| 2 | `usage` | Bad arguments or an invalid tunnel specification (`invalid_spec`), or an unknown or ambiguous tunnel |
| 3 | `needs_secret` | A required secret is missing, or the keychain is unavailable |
| 4 | `needs_user` | A prompt needs a person and no TTY is available (or `--no-input`). Includes `pending_approval`; poll the approval and retry after a person decides. |
| 5 | `conflict` | Port in use (`port_in_use`), or `tunnels.toml` changed on disk |
| 6 | `timeout` | Not ready within the timeout |
| 7 | `daemon` | The daemon cannot be reached or started, or its version is incompatible |
| 8 | `not_found` | A required binary is missing, e.g. `ngrok` |

Keep this table in step with `internal/api` error codes, which MCP also uses.

### Prompts

On a TTY, `devt-tunnel start`, `run` and `forward` show SSH questions for their own tunnel inline, as `ssh` would, and `rm` asks before deleting. Without a TTY, or with `--no-input`, nothing waits for input: the command exits with code 4 and the prompt stays available through `devt-tunnel prompts` and the app until the broker times out.

### Configuration

| Source | Used for |
|---|---|
| `tunnels.toml` | Saved tunnels, profiles and groups |
| `settings.toml` | Per-machine tool paths and the `[agent]` policy (`default_expose` is not implemented) |
| `DTT_SOCKET` | Use a different daemon socket. For tests |
| `NO_COLOR` | Turn off color |

There are no environment variables for secret values.

## How the documentation is built

`--help`, the [command reference](../cli/reference/devt-tunnel.md), man pages and completions are generated from the `cobra` definitions in `internal/cli` with `go run ./internal/cli/gen`. Guides in `docs/cli/*.md` are hand-written and embedded for `devt-tunnel docs`; `agents.md` is also an MCP resource.

CI enforces:

- **Drift:** the regenerated reference must match what is committed.
- **Examples:** shell blocks and inline `devt-tunnel ...` examples in the guides are parsed against the command tree.
- **Exit codes:** the tables here and in the troubleshooting guide must match `ExitCodes`, which must cover every `internal/api` error kind.
- **Completeness:** every command needs `Long` text and an `Example`.
