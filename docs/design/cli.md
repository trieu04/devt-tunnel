# `dtt` command line

Status: **accepted 2026-11-04; phase 18 implemented except phase-19-dependent commands; native/Linux headless exit test not yet run.** Saved-tunnel read, lifecycle, prompts, define, organize and setup commands are available, including shell completion, embedded guides and deterministic generated reference/man pages. `run`, `forward`, `expose`, `save`, `leases`, `approvals` and `audit` wait for phase 19; daemon login installation and MCP remain future work. `daemon run` supports `--idle-exit` (default 5m, 0 disables). Architecture and rationale are in [control-plane.md](control-plane.md).

This file is the interface contract. User-facing guides and the generated reference are in [docs/cli](../cli/quickstart.md); see [Documentation plan](#documentation-plan) for generation and CI enforcement.

## User guides

- [Quick start](../cli/quickstart.md): install, saved tunnels, planned one-off commands, completion and offline help.
- [Replacing traditional commands](../cli/replacing.md).
- [Scripts](../cli/scripts.md).
- [Troubleshooting and exit codes](../cli/troubleshooting.md).
- [Generated command reference](../cli/reference/dtt.md).

## Concepts

- **Tunnel:** one supervised process. **Saved** tunnels are in `tunnels.toml`. **Ephemeral** tunnels, with ids starting `tmp-`, come from `run`, `forward` and `expose`, and are removed when they stop.
- **Daemon:** a small background process that owns all tunnels. Any command starts it when needed. It exits when idle. The desktop app uses the same daemon, so both show the same tunnels.
- **Referring to a tunnel:** by exact id, exact name or a unique id prefix. An ambiguous reference exits with code 2 and lists the matches.

## Commands

### Find and inspect

| Command | Purpose | Main flags |
|---|---|---|
| `dtt ls` | List tunnels | `--running` `--type T` `--profile P` `--favorites` |
| `dtt show <t>` | Definition, state, endpoints, command, leases, findings | |
| `dtt addr <t>` | Print only the address | `--port` `--all` |
| `dtt logs <t>` | Retained output | `-f` `-n N` `--since D` |
| `dtt wait <t>` | Block until running | `--timeout D` |
| `dtt check <t>` | Run the editor checks | `--strict` makes warnings fail |
| `dtt hosts` | SSH aliases from config | `--resolve <alias>` `--profile P` |
| `dtt doctor` | Binaries, keychain, daemon, SSH config, shell PATH | |

### Run

| Command | Purpose | Main flags |
|---|---|---|
| `dtt start <t>...` | Start saved tunnels and wait until ready | `--no-wait` `--timeout D` |
| `dtt stop <t>...` | Stop now, revoking any leases | `--all` `--profile P` |
| `dtt restart <t>...` | Stop, then start | |
| `dtt run -- <cmd>` | Supervise a traditional command, in the foreground | `--detach` `--ttl D` `--name N` `--dry-run` |
| `dtt forward <host> [map...]` | Ephemeral SSH tunnel, in the background | `--remote M` `--socks P` `--ttl D` `--foreground` `--save NAME` `--dry-run` |
| `dtt expose <port\|url>` | Ephemeral public tunnel | `--via ngrok\|cloudflared` `--domain D` `--secret S` `--ttl D` `--foreground` `--save NAME` `--dry-run` |

Port maps use the form `listen:target`:

| Form | Means |
|---|---|
| `5433:5432` | `127.0.0.1:5433` to port 5432 on the far side's loopback |
| `5433:db:5432` | `127.0.0.1:5433` to `db:5432` as seen from the SSH host |
| `0.0.0.0:5433:db:5432` | Listen on all interfaces. Prints a warning |
| `auto:5432` | Pick a free local port, and print it |

`dtt expose` has no default for `--via` unless `default_expose` is set in `settings.toml`, because the two services differ in cost and in who can reach the URL. It always prints a line saying the service will be public.

`--detach` and background forms return once the tunnel is ready, or exit with an error. `--ttl` stops the tunnel after that time. Ctrl-C in the foreground stops the tunnel.

### Define

| Command | Purpose |
|---|---|
| `dtt add ssh <name> --host H -L M [-R M] [-D P] [--flag F] [--option K=V]` | Save an SSH tunnel |
| `dtt add ssh <name> --hostname H --user U --port N --identity PATH ...` | Save without a config alias |
| `dtt add ngrok <name> --proto http --addr A [--domain D] [--secret S]` | Save an ngrok tunnel |
| `dtt add cloudflared <name> --url U` / `--tunnel-name N --hostname H --secret S` | Save a quick or named tunnel |
| `dtt add command <name> --line L [--dir D] [--ready-log RE] [--secret-env K=S]` | Save any command |
| `dtt import [--name N] -- <cmd>` | Save from a traditional command, showing warnings |
| `dtt save <tmp-id> --name N` | Turn an ephemeral tunnel into a saved one |
| `dtt edit <t>` | Open a TOML fragment in `$EDITOR`, then validate it |
| `dtt set <t> key=value...` | Change fields without an editor, e.g. `favorite=true reconnect=false` |
| `dtt dup <t> --name N` | Copy a tunnel |
| `dtt rm <t>` | Delete, asking first on a TTY. `--yes` is required otherwise |

All of `add` take `--profile P`, `--favorite`, `--start-on-launch`, `--no-reconnect`, `--start` and `--dry-run`. `--dry-run` prints the exact command and the check results without saving.

### Profiles

| Command | Purpose |
|---|---|
| `dtt profile ls` / `show <p>` | List and inspect |
| `dtt profile add <name> [--ssh-config F] [--on-activate filter\|start_autostart] [--stop-non-members]` | Create |
| `dtt profile use <p>` / `use --none` | Set the active profile, running its activation behavior |
| `dtt profile start <p>` / `stop <p>` | Start or stop members |
| `dtt profile autostart <p> <t> on\|off` | Set auto-start for a member |
| `dtt profile rm <p>` | Delete. Tunnels are kept |

### Secrets

Values are read from a hidden prompt or from stdin, never from a flag, so they stay out of shell history and `ps`.

```bash
dtt secret set ngrok-personal --kind ngrok
```

```bash
op read op://dev/ngrok/token | dtt secret set ngrok-personal --kind ngrok --stdin
```

| Command | Purpose |
|---|---|
| `dtt secret ls` | Names, kinds, users and last use. No values |
| `dtt secret set <name> --kind ssh_passphrase\|ngrok\|cloudflared\|env` | Store a value. Starts that were waiting on it retry |
| `dtt secret rename <from> <to>` | Rename, and update the tunnels using it |
| `dtt secret rm <name>` | Delete. Tunnels using it show `needs_token` |

There is no `reveal`. It requires OS authentication, which the desktop app provides.

### Oversight

| Command | Purpose |
|---|---|
| `dtt prompts` / `dtt prompts answer <id>` | SSH questions waiting for a person |
| `dtt approvals` / `approve <id>` / `deny <id>` | Agent requests. `approve` needs a TTY and the code shown in the request |
| `dtt leases` / `dtt leases revoke <id>` | Which clients hold which tunnels, until when |
| `dtt audit` | History of changes. `--actor A` `--tunnel T` `--since D` `-f` |
| `dtt orphans` / `dtt orphans stop <pid>` | Processes left by a crash |

### Setup

| Command | Purpose |
|---|---|
| `dtt daemon status\|stop\|restart` | Control the daemon. `stop` also stops tunnels and says so first |
| `dtt daemon install [--start-marked]` / `uninstall` | Run at login. Optional |
| `dtt daemon run` | Run in the foreground. For service units and debugging |
| `dtt config path\|reload\|edit` | Work with `tunnels.toml` |
| `dtt tools` / `dtt tools set <tool> <path>` | Binaries used, with per-machine paths |
| `dtt mcp serve` / `config` / `tools` | MCP server, see [mcp.md](mcp.md) |
| `dtt completion bash\|zsh\|fish\|powershell` | Shell completion, including tunnel IDs/names and profile names; never starts a daemon |
| `dtt docs [topic]` / `dtt man [--dir DIR]` | Embedded offline guides / section 1 pages (default directory: current) |
| `dtt version` | CLI and daemon versions |

## Global behavior

### Flags

`--json` · `-q, --quiet` · `--no-color` (also `NO_COLOR`) · `--timeout D` · `--no-daemon-start` · `--no-input`.

### Output

- Results go to stdout and diagnostics to stderr. Tables are for people and may change.
- `--json` is the stable interface. Every object has `"apiVersion": 1`. New fields may appear, and removing or renaming a field raises the version.
- Colors, spinners and tables appear only on a TTY.

```bash
dtt ls --running --json
```

```json
{
  "apiVersion": 1,
  "tunnels": [
    {
      "id": "db",
      "name": "Production DB",
      "type": "ssh",
      "state": "running",
      "pid": 4242,
      "endpoints": [{ "kind": "local", "listen": "127.0.0.1:15432", "target": "127.0.0.1:5432", "public": false }],
      "owner": { "pinned": true, "ephemeral": false, "leases": 0 }
    }
  ]
}
```

`ls --json` and `show --json` include `owner` with `pinned`, `ephemeral` and `leases`; active claims also include a holder and expiry. `logs --json -f` emits one versioned object per batch (newline-delimited JSON). Log lines currently have sequence numbers but no timestamps; a positive `--since` is rejected with exit 2 rather than silently ignoring the filter.

With `--json`, errors are written to stderr as a single object:

```json
{ "apiVersion": 1, "error": { "code": "needs_secret", "message": "secret \"ngrok-personal\" is missing", "tunnel": "web", "hint": "dtt secret set ngrok-personal" } }
```

### Exit codes

Scripts can rely on these.

| Code | Name | Meaning |
|---|---|---|
| 0 | | Success |
| 1 | `failed` | The tunnel or operation failed, including a person revoking a lease (`lease_revoked`) or a policy refusal (`policy_denied`). See the message and `dtt logs db` |
| 2 | `usage` | Bad arguments, or an unknown or ambiguous tunnel |
| 3 | `needs_secret` | A required secret is missing, or the keychain is unavailable |
| 4 | `needs_user` | A prompt needs a person and no TTY is available (or `--no-input`). Includes `pending_approval`; poll the approval and retry after a person decides. |
| 5 | `conflict` | Port in use, or `tunnels.toml` changed on disk |
| 6 | `timeout` | Not ready within the timeout |
| 7 | `daemon` | The daemon cannot be reached or started, or its version is incompatible |
| 8 | `not_found` | A required binary is missing, e.g. `ngrok` |

Keep this table in step with `internal/api` error codes, which MCP also uses.

### Prompts

On a TTY, `dtt start`, `run` and `forward` show SSH questions for their own tunnel inline, as `ssh` would, and `rm` asks before deleting. Without a TTY, or with `--no-input`, nothing waits for input: the command exits with code 4 and the prompt stays available through `dtt prompts` and the app until the broker times out.

### Configuration

| Source | Used for |
|---|---|
| `tunnels.toml` | Saved tunnels and profiles |
| `settings.toml` | Per-machine tool paths, the `[agent]` policy, `default_expose` |
| `DTT_SOCKET` | Use a different daemon socket. For tests |
| `NO_COLOR` | Turn off color |

There are no environment variables for secret values.

## Scripts and troubleshooting

The user-facing [scripts](../cli/scripts.md) and [troubleshooting](../cli/troubleshooting.md) guides live in `docs/cli/` and are embedded for offline use with `dtt docs`.

## Documentation plan

Documentation comes from the command definitions where possible, so that it cannot drift.

| Output | Source | Where |
|---|---|---|
| `dtt <cmd> --help`: synopsis, flags, 1 to 3 examples, the traditional equivalent, exit codes | `cobra` command definitions in `internal/cli` | Terminal |
| Command reference | Generated by `cobra/doc` with `go run ./internal/cli/gen` | `docs/cli/reference/*.md`, committed |
| Man pages | Generated by `cobra/doc` with `go run ./internal/cli/gen` | Ignored `bin/man/*.1` for packaging (not committed), and `dtt man --dir DIR` |
| Shell completion, with dynamic tunnel ids | `cobra` | Packages, and `dtt completion` |
| Guides: quick start, replacing commands, scripts, troubleshooting | Hand-written in `docs/cli/` | `docs/cli/*.md`, embedded with `go:embed` for `dtt docs` |
| Agent guide | Hand-written | `docs/cli/agents.md`, also served as an MCP resource |

Enforcement in CI:

- **Drift:** regenerate the reference, and fail if `git diff` is not empty.
- **Examples work:** a test parses shell blocks and inline `dtt ...` examples in `docs/cli/*.md` against the command tree without executing them. Future `run`, `forward`, `expose` and `save` examples are explicitly marked `phase19` and available after phase 19; only those unavailable calls are skipped. A renamed flag fails the build.
- **Exit codes:** a test confirms the tables above and in the embedded troubleshooting guide match `ExitCodes`, which must cover every `internal/api` error kind.
- **Completeness:** a test fails when a command has no `Long` text or `Example`.

The README links the install/usage guide. Reference Markdown is committed; man pages are generated at package time. Automatic date/history tags are disabled and the man header date is fixed to 2026-11-04, so output is deterministic. The guides retain future examples rather than silently dropping the phase-19 migration path.
