# Central tunnel management

Status: **accepted 2026-11-04; phases 14–16 engine, core fixes and daemon/API implemented; native-window acceptance pending.** Phase 16 implements the daemon, local API and `dtt daemon` lifecycle commands. The GUI still hosts its engine in-process until phase 17; the full CLI and MCP remain planned. The target design was written against commit `956c869` and rechecked against `6e0028f`; [phases 14–21](../plans/README.md#phases) deliver it.

Companion contracts: [cli.md](cli.md) (`dtt` command line and its documentation) and [mcp.md](mcp.md) (tools for agents).

## Problem

Tunnels live in several places at once. People run `ssh -L`, `ngrok` and `cloudflared` in terminals, the desktop app runs its own set, and coding agents start background `ssh` processes that nobody tracks. Nothing answers "what is open on this machine, who opened it, and when does it close?". Ports collide, public tunnels are forgotten, and agents leave processes behind.

## Goals

- One registry and one supervisor per user per machine. The desktop app, the CLI and agents all see the same tunnels and states.
- A CLI that replaces the traditional commands for daily use, including on machines with no desktop.
- An MCP server that lets agents create and use tunnels without shell access, with limits they cannot change through MCP.
- Every agent-opened tunnel has an owner and an expiry, and is cleaned up without human help.

## Non-goals

- Multi-machine or team management: no shared server, no remote control, no accounts. "Central" means central on one machine, see [Decisions](#decisions).
- Isolating a hostile agent that runs as the same OS user, see [Threat model](#threat-model).
- Replacing `ssh`, `ngrok` or `cloudflared`. They remain the system binaries the supervisor spawns.
- Interactive shells. `ssh` for a login session stays as it is.

## Design in brief

Move the core out of the desktop process into a small per-user daemon. The GUI, the CLI and the MCP server become clients of one local API.

```text
 Desktop app             dtt (CLI)            Agent client (Claude Code, Cursor, ...)
 React UI, tray,           │                    │ MCP over stdio
 shortcut, OS auth         │                    ▼
 internal/host             │                  dtt mcp serve
       │                   │                    │
       └───────────────────┴──── JSON-RPC ──────┘
                           │     local socket, same user only
                           ▼
 ┌─ dtt daemon (no Wails, no GUI libraries) ─────────────────────────┐
 │  internal/control  API server, client identity, event stream     │
 │  internal/engine   registry · leases · policy · approvals · audit │
 │  internal/core     model · config · command · checks · supervisor │
 │  internal/askpass  prompt broker                                  │
 │  internal/platform processes · keychain · paths                   │
 └──────┬──────────────────────────────────────────▲─────────────────┘
        │ spawn                                    │ per-spawn token
        ▼                                          │
   ssh · cloudflared · ngrok · command ──► devt-tunnel-askpass
```

`tunnels.toml` stays the source of saved definitions. The daemon is its only writer in normal use; `config.Store` already detects hand edits and returns `ErrConflict`.

## Why a daemon

Today every tunnel is a child of the GUI process, `internal/engine` owns the wiring, and the Wails `SingleInstance` option blocks a second process. [platform.md](../platform.md) anticipated this: "If a CLI or background mode is wanted later, the core can move behind a local API then".

| Option | Headless server | Survives GUI quit | Code paths | Verdict |
|---|---|---|---|---|
| A. The GUI hosts the API; the CLI launches the GUI when it is not running | No: needs a display, GTK and WebKit | No | One | Fails the "machines with no desktop" goal, and every CLI call could open an app |
| B. Whichever starts first hosts the engine; the GUI runs in-process or as a client | Yes | Depends on who started first | Two for every GUI call | Most bugs would appear only in one mode |
| **C. Always a daemon; the GUI is always a client** | Yes | Yes | One | **Recommended** |

Option C costs the most at first, because the GUI must move to the API at once. It leaves one path to test. `host.Service` keeps its method signatures and forwards them to the API, so `frontend/bindings` and the React code should change little.

### Daemon lifecycle

- **Binary:** `dtt` is one Go binary with no Wails dependency, built with `CGO_ENABLED=0` as the core tests already are. `dtt daemon run` is the daemon. Packages ship it next to the app, as `devt-tunnel-askpass` is today.
- **Start:** any client starts it on demand, detached from the caller (`setsid` on Unix, `DETACHED_PROCESS` on Windows). `--no-daemon-start` turns this off.
- **Single instance:** an advisory lock on `daemon.lock` beside the socket decides ownership. The socket alone cannot, since a stale socket file can remain after a crash. A second daemon exits at once, so two clients starting one at the same moment both end up connected to the winner.
- **Logs:** the daemon writes its own `daemon.log`, because two processes cannot safely rotate the one `devt-tunnel.log`.
- **Idle exit:** the daemon exits after 5 minutes with no active tunnel, lease, pending prompt or connected client. There is no permanent background process unless the user runs `dtt daemon install` (launchd or systemd user unit; Windows later).
- **Upgrade:** matching API/build is reused. A different build with the same API stays running while active, and an idle daemon is replaced. A different API is replaced only when idle; active incompatibility names `dtt daemon restart`, which stops all tunnels. The server rechecks `onlyIfIdle` during shutdown to prevent an upgrade racing a new tunnel start.
- **Crash:** the existing recovery still applies. `processes.json` and `FindOrphans` find surviving children on Unix, where children run in their own process group. On Windows, `KILL_ON_JOB_CLOSE` means children die with the daemon, so there are no orphans, and leases are restored as stopped.

### What changes in the current code

| Today | Problem for this design | Change |
|---|---|---|
| ⌘Q and quit stop all tunnels (`Service.Shutdown`) | Needs a meaning when the CLI and agents also hold tunnels | ⌘Q still means "stop all and quit": it calls `daemon.shutdown`, after a dialog naming any agent leases. Closing the window with a tray only hides it, as now |
| `StartOnLaunch` runs in `Attach` on app start | If moved as is, a plain `dtt ls` would autostart tunnels by launching the daemon | The GUI requests these starts once at launch. The daemon starts nothing by itself. `dtt daemon install --start-marked` opts in for headless machines |
| Wake events come from Wails (`SystemDidWake`) | The daemon has no Wails | Move `monitorClockJump` to the daemon. The GUI forwards OS wake events with `system.wake` |
| `askUser` emits a Wails event and shows the main window | Blocks 2 minutes with no reader on a headless daemon | Prompts become a daemon queue that any connected UI or TTY can answer, see [Prompts](#prompts) |
| `secrets.Forget()` runs on window focus | The cache moves into the daemon | The GUI calls `secrets.forget` on focus |
| `RevealSecret` runs after OS auth in-process | A secret value would cross the API | Reveal stays in the GUI: authenticate, then read the keychain directly. The API has no method that returns a value |
| `Manager.Start` with a missing secret returns `nil` and sets `stopped`; `WaitReady` then returns an empty error | The CLI and MCP cannot tell what happened | Keep `Start` as it is, because the Vault retry flow relies on it. `WaitReady` returns typed `ErrNeedsSecret` and `ErrStopped` |
| `settings.toml` is rewritten whole from one in-memory copy | Two writers would overwrite each other's fields | All writes go through the daemon, including the GUI's shortcut and update-check settings |
| Shared types and engine were coupled to `host` | The CLI and daemon must not link Wails | Phase 14 moved them to `internal/api` and `internal/engine`; bindings now import the shared types, and the host forwards in-process |
| `checks.Deps.Listening` is never set outside tests | "Port already in use" never fires, and allocation needs it | Wire it in the engine |
| `Status.Address` holds one address | Tunnels may have many forwards | Add `Endpoints`, see [API](#local-api) |
| `SSH.Flags`, `SSH.Options` and `SSH.ExtraArgs` have no check for command execution | `ProxyCommand` and `LocalCommand` run programs. Acceptable from the owner; not from an agent | New `checks.AgentSafe`, see [Policy](#policy) |
| `Manager.SetConfig` stops any tunnel missing from the config | Ephemeral tunnels would be killed on the next save | The engine always passes saved plus ephemeral definitions |

## Tunnel ownership

The engine adds three concepts on top of the `Tunnel` model. Saved definitions do not change.

| Concept | Meaning | Stored in |
|---|---|---|
| Saved tunnel | Today's tunnel. Shareable definition | `tunnels.toml` |
| Ephemeral tunnel | Created by `dtt run`, `forward`, `expose` or an agent. Ids start with `tmp-`. Removed when it stops. `dtt save` or `tunnel_save` promotes it to a saved tunnel | `runtime.json` (state directory) |
| Lease | A claim by one client to use one tunnel until a set time | `runtime.json` |

Agents do not write to `tunnels.toml` unless a person approves, because that file is meant to be reviewed and shared.

```go
type Lease struct {
    ID         string    // "ls_..."
    TunnelID   string
    Holder     Actor     // kind: "gui" | "cli" | "mcp"; name: e.g. "claude-code"
    Session    string    // the client connection that opened it
    CreatedAt  time.Time
    ExpiresAt  time.Time
    StartedIt  bool      // this lease caused the start
}
```

### Rules

1. **A person starting a tunnel pins it.** GUI or CLI `start` sets `pinned`. No lease expiry stops a pinned tunnel.
2. **Opening adds a lease and starts the tunnel if needed.** Two agents can share one running tunnel, each with its own lease.
3. **Closing or expiring removes a lease.** The tunnel stops when no leases remain, it is not pinned, and a lease started it. A tunnel that was already running stays up.
4. **A person always wins.** `dtt stop` and the GUI stop the tunnel at once and revoke its leases. The agent's next call returns `lease_revoked`. An agent can only release its own lease, never stop a tunnel directly.
5. **Limits:** default TTL 30 minutes, maximum 8 hours, renewable up to the maximum per renewal. When an MCP client's connection drops, its leases end after a 2-minute grace period, so a restarted adapter can continue.
6. **Crash safety:** leases and ephemeral definitions are written before the spawn. After a daemon restart, expired ones are stopped and removed; the rest continue if the process survived.

### Port allocation

Agents and `dtt forward` may request `auto` for a local port. The engine takes a port from `agent.port_range` (default `20000-20999`) that no tunnel uses and `Listening` reports free, and binds only to `127.0.0.1`. A process can still take the port between the check and the spawn. `ExitOnForwardFailure=yes` already turns that into an exit, so the engine retries with another port, up to 3 times.

## Local API

JSON-RPC 2.0, one message per line, over a Unix socket. Go's `AF_UNIX` is already used for askpass on all three operating systems. gRPC or HTTP would add dependencies and no benefit for one local socket.

- **Location:** `$XDG_RUNTIME_DIR/devt-tunnel/ctl.sock` on Linux when set, otherwise `<state dir>/run/ctl.sock`, in a directory with mode `0700` and an owner check. Socket paths are limited to about 100 bytes, so a longer path falls back to a per-user `0700` directory under the OS temp directory.
- **Authorization:** directory permissions and, on Unix, a peer UID check (`SO_PEERCRED`, `LOCAL_PEERCRED`). On Windows, the per-user ACL of the directory under `%LOCALAPPDATA%`. Whether that is enough on Windows needs testing in phase 12.
- **Handshake:** `daemon.hello {apiVersion, client: {kind, name, version}}` returns `{apiVersion, daemonVersion, pid}`. `kind` selects the policy and is recorded in the audit log. A client declares it itself, see [Threat model](#threat-model).
- **Version recovery:** an incompatible hello still returns version metadata and permits only `daemon.status` and `daemon.shutdown`; normal client dialing rejects it. `Ensure` uses these recovery methods to implement the upgrade rule.
- **Errors:** JSON-RPC errors carry `data.kind`: `conflict`, `needs_secret`, `keyring_unavailable`, `stopped`, `unknown`, `internal`, `incompatible`, or `timeout`. Secret values enter only through `secret.set` or credential-bearing `prompt.answer`; neither returns a value, and request parameters are never logged. `daemon.shutdown` accepts the handshake PID as `expectedPid` and rejects a different daemon identity, so a lost response cannot make a retry stop a replacement process.
- **Types:** one package, `internal/api`, used by every client. New fields are additive, and a breaking change raises `apiVersion`.

| Group | Methods |
|---|---|
| `tunnel.*` | `list` `get` `save` `delete` `start` `stop` `restart` `evaluateDraft` `import` `newDraft` `duplicateDraft` `setFavorite` `promote` |
| `lease.*` | `open` `renew` `close` `list` |
| `logs.*` | `read` `since`. Clients poll `since`, as `WatchLogs` polls `LogsSince` today |
| `profile.*` | `list` `save` `delete` `duplicate` `activate` `setAutostart` `start` `stop` |
| `secret.*` | `list` `set` `delete` `rename` `forget`. Values go in only |
| `prompt.*` | `list` `answer` |
| `approval.*` | `list` `decide` |
| `ssh.*`, `tool.*`, `settings.*`, `orphan.*`, `config.*` | The rest of today's `host.Service`, excluding window and OS auth methods |
| `events.subscribe` | Stream: `tunnels.changed`, `prompt.added`, `approval.added`, `lease.expiring` |
| `daemon.*`, `system.*` | `hello` `status` `shutdown`; `wake` |

Phase 16 also exposes `tunnel.waitReady`, `tunnel.findings`, `tunnel.recordUsed`, `tunnel.startOnLaunch`, `config.overview`, `system.focus` and the current engine settings methods. `internal/api.Methods` is the tested registry. `tunnel.promote`, leases and approvals arrive in phase 19. Each connection handles concurrent requests, cancels their contexts on disconnect, and drops slow event consumers; clients subscribe then resync from `config.overview` after reconnect.

The response for a tunnel adds fields to today's `TunnelView`:

```jsonc
{
  "tunnel": { "id": "db", "name": "Production DB", "type": "ssh" /* ... */ },
  "status": { "state": "running", "pid": 4242, "startedAt": "2026-11-04T10:00:00Z", "error": "" },
  "endpoints": [
    { "kind": "local", "listen": "127.0.0.1:5433", "target": "127.0.0.1:5432", "public": false }
  ],
  "owner": { "pinned": false, "ephemeral": false, "leases": 1 },
  "preview": "ssh -N -o ... -L 127.0.0.1:5433:127.0.0.1:5432 server1",
  "missingSecrets": []
}
```

`endpoints` is additive and is always an array. SSH forwards preserve bind and target strings with kinds `local`, `remote` and `socks`; Unix socket and loopback binds are not public. Ngrok and cloudflared endpoints are `public` with `public: true`, and `listen` stays empty until the public address is known (ngrok's local-target fallback is excluded). Command tunnels expose configured forwards or an address announced by their readiness log; otherwise the array is empty. `public` describes the bind, while agent policy still requires approval for every remote forward.

`WaitReady` returns `supervisor.ErrNeedsSecret` for a pending secret start or `needs_token`, `secret.ErrUnavailable` for `keyring_unavailable`, and `supervisor.ErrStopped` for an ordinary stop. Failed starts retain the status error with a nonempty fallback. `Manager.Start` and Vault retries are unchanged.

Editor draft checks and saved-tunnel findings probe loopback TCP listeners with a 100 ms timeout. They exclude the active tunnel's own configured local ports; editing it to use a different occupied port still warns.

`preview` still comes from the same builder as the spawn, so the guarantee that the preview equals execution holds for all clients.

## Prompts

`SSH_ASKPASS_REQUIRE=force` sends every prompt through the broker, including host-key confirmation and OTP. The daemon queues each one, and the following clients can see it:

- **GUI:** the existing dialog.
- **`dtt start` on a TTY:** asks inline for its own tunnel, like plain `ssh`.
- **`dtt prompts`:** answers from any terminal.
- **MCP:** can see that a prompt is waiting, and its kind, but **cannot answer**. A host-key or password answer from an agent would defeat the prompt's purpose.

The 2-minute broker timeout stays. A non-TTY CLI call or an agent gets `needs_user`, and the tunnel fails at the timeout if nobody answers.

## Policy

Policy is per-machine, in `settings.toml`. MCP has no tool to change it.

```toml
[agent]
enabled = true
port_range = "20000-20999"
default_ttl = "30m"
max_ttl = "8h"
allow_tunnels = ["dev-*"]          # saved tunnels usable without approval
allow_ssh_hosts = ["staging"]      # aliases usable for new local forwards without approval
public = "ask"                     # ask | deny
```

| Agent action | Default | Can allow-list? |
|---|---|---|
| List tunnels and SSH aliases, read redacted logs, dry-run checks | Allow | |
| Open a saved tunnel | Ask | `allow_tunnels` |
| New `-L` or `-D` on `127.0.0.1` through a config alias | Ask | `allow_ssh_hosts` |
| SSH by manual hostname rather than an alias | Ask | No |
| Public exposure: ngrok, cloudflared, any `-R`, any non-loopback bind | Ask, or deny by setting | **No** |
| Save a tunnel to `tunnels.toml` | Ask | No |
| Use an existing secret by name for a new tunnel | Ask | No |
| Create or edit `command` tunnels, `flags`, `options`, `extra_args`, `identity` | **Deny**; not in the schema | No |
| Read, set, rename or delete secret values; answer prompts; edit policy; edit or delete saved tunnels; stop tunnels without a lease | **Deny**; no tool exists | No |

`checks.AgentSafe` also validates every agent-built tunnel on the daemon side, including a saved tunnel when it is opened. The MCP schema alone is not trusted. A saved `command` tunnel, or a saved tunnel whose options include `ProxyCommand`, `LocalCommand` or `PermitLocalCommand`, always needs approval even when its id matches `allow_tunnels`, because its command runs on open. Aliases in the user's own `ssh_config` are trusted, since the owner wrote them.

### Approvals

A request that needs approval returns `pending_approval` at once. Long-blocking tool calls are not used because clients time them out.

1. The daemon records the actor, the summary, the exact argv from `command.Build`, and `Spec.Fingerprint()`.
2. The GUI shows a dialog, and `dtt approvals` lists it. If nobody decides within 5 minutes, it is denied.
3. The choices are **once**, **for this session**, and **always**, which adds the tunnel id or alias to the allow-list. "Always" is not offered for public exposure.
4. On approval, the daemon runs only the recorded fingerprint. A changed spec needs a new approval.

`dtt approvals approve` requires an interactive terminal and a short code shown by the request. That only slows an agent with shell access; see the next section.

## Threat model

| Threat | Mitigation | Limit |
|---|---|---|
| Other OS users on the machine reach the socket | `0700` directory, peer UID check | Root can always reach it |
| An agent with **only MCP** exposes a port publicly by mistake, or after prompt injection | Approval is always required; the exact argv is shown to a person | A person can approve carelessly |
| An agent with only MCP runs commands through tunnel options | Fields are absent from the schema, and `AgentSafe` rejects them on the daemon | |
| Tunnel output contains a prompt injection | Logs are marked `untrusted` in the tool result and returned only on request | The client decides how to treat them |
| Tunnel output contains a secret | Known secret values are replaced by `[secret:name]` before logs leave the daemon for an MCP client | Secrets the daemon does not know are not caught |
| Forgotten agent tunnels | TTL, connection-drop release, a visible badge, `dtt leases` | |
| Secrets leaking through argv or API | `secret set` reads stdin or a hidden prompt; no API method returns a value | |

**The limit that matters: this is not a security boundary against an agent with a shell.** It runs as the same user, so it can call `dtt` as a "cli" client, edit `settings.toml`, or run `ssh -R` directly. No local design fixes this without a separate OS user or sandbox. The policy protects the MCP route, for agents without shell access or with a client that approves each shell command. It also gives shell-capable agents a tracked option that is easier than a stray `ssh`. Documentation must say this plainly.

## Audit

`audit.jsonl` in the state directory, rotating like the app log. One line per state-changing call and approval decision: time, actor, action, tunnel id, fingerprint, decision, result. No secret values and no log content. `dtt audit` reads it. It is a record, not tamper-proof.

## GUI changes

Small, and using existing screens:

- Tunnel rows and detail panel: an agent badge with the holder name and time left, and an `ephemeral` tag with **Save**.
- Approval dialog, built on the SSH prompt dialog, showing actor, summary and argv.
- Settings: an **Agents** section for the policy, and **Install command-line tool**.
- Connection errors: a banner and a retry if the daemon cannot be reached.

## Rollout

Tasks, tests and order are in the [implementation plans](../plans/README.md#phases), phases 14–21. Phase 17 is the riskiest and needs native-window acceptance on macOS, since `go test` cannot show that the tray, dialogs and quit still work.

### Exit tests

- **Daemon:** with the app closed, `dtt start db` opens a working tunnel. Opening the app shows it running. ⌘Q stops it, and the daemon exits.
- **Headless:** on a Linux machine with no GTK or display, `dtt` installs from one binary, opens an SSH forward and answers a host-key prompt on the TTY. On a machine without Secret Service, secret-free tunnels work, and tunnels with secrets report `keyring_unavailable`.
- **Agent:** a real MCP client with no shell tool opens a forward to an allow-listed alias, connects through it and closes it. The process is gone afterward. Killing the client ends the lease after the grace period.
- **Policy:** the same client asking for a public tunnel is held until a person approves in the GUI, and a request with `ProxyCommand` is refused at the daemon even when sent directly to the socket with `kind: "mcp"`.

### Risks

- **Keychain from a background process.** Still unverified: the macOS library cannot target a disposable keychain without altering user keychain selection. No real entries were used for phase 16. Secret Service without a desktop, login timing and signed/hardened-runtime access also need acceptance. Tunnels that need secrets report `keyring_unavailable`.
- **Agent environment.** A Finder-launched disposable app and its detached child inherited the graphical launchd agent socket on macOS. A private localhost SSH-forwarding probe retained a stale session socket path after disconnect. The daemon keeps its launch environment; restart it from the desired local/desktop session to inherit a persistent agent. It does not silently replace `SSH_AUTH_SOCK`.
- **macOS signing.** A second signed executable inside the bundle, launched detached, needs checking under hardened runtime and notarization.
- **Tunnels that run with no window.** Today's code avoids invisible tunnels on purpose (`WindowClosing` quits without a tray). The daemon reverses that for CLI use. Mitigation: `dtt ls`, `dtt daemon status`, and the GUI shows everything on launch.
- **MCP changes quickly.** The 2026-07-28 revision is a large change. Keep MCP in a thin adapter so that SDK upgrades do not reach the engine.

## Decisions

Accepted by the project owner on 2026-11-04.

| # | Decision | Consequence |
|---|---|---|
| C1 | An on-demand daemon replaces "one process, no daemon" | The GUI, CLI and MCP are all clients. architecture.md and platform.md are updated |
| C2 | "Central" means per user, per machine | The API has no remote transport. A team or fleet product would need its own design: server, accounts, secret distribution |
| C3 | ⌘Q keeps stopping everything, including CLI and agent tunnels | Today's product rule stays. The tray could later add "Close app, keep tunnels" |
| C4 | Public exposure by an agent always needs a person | Unattended agents cannot publish local services |
| C5 | The CLI is named `dtt` and uses `cobra` | `devt-tunnel` remains the app. `cobra` is a new dependency, used for generated docs and completions |

Move `cli.md` content into user documentation as commands ship.

## Sources

Checked 2026-11-04: [MCP specification release 2026-07-28](https://github.com/modelcontextprotocol/modelcontextprotocol/releases/tag/2026-07-28), [go-sdk v1.8.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0), [cobra v1.10.2](https://github.com/spf13/cobra/releases/tag/v1.10.2).
