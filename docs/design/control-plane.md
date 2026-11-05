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

The implemented `runtime.json` is versioned (`version: 1`) and contains `ephemeral`, `leases`, and per-tunnel `owners` (`pinned`, `leaseStarted`). Optional `autoPorts` records allocated forward indices; `promotions` is a write-ahead old-id/new-id map for recovery across `runtime.json`, `tunnels.toml` and process-record renames. Person-revoked leases remain as tombstones until their original expiry so the next renew/close returns `lease_revoked`. Promotion changes the registry id in place, retaining the PID and command fingerprint. Surviving Unix children remain in the existing orphan list rather than being silently respawned; expired lease-owned orphans are stopped. Runtime writes use a synced, mode-`0600` temporary file and atomic rename before a spawn.

```go
type Lease struct {
    ID         string    // "ls_..."
    TunnelID   string
    Holder     Actor     // kind: "gui" | "cli" | "mcp"; name: e.g. "claude-code"
    Session    string    // reconnect token returned by daemon.hello, shared by this client's connections
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
5. **Limits:** MCP sessions may hold at most 32 live leases in total, including ephemeral-tunnel leases. Overlapping connections with the same session token share that cap. Person clients are not subject to this count limit. Default TTL 30 minutes, maximum 8 hours, renewable up to the maximum per renewal. When a lease-holding client's last connection drops, its leases end after a 2-minute grace period, so a restarted adapter can continue. Reconnect with the same `client.session` token and the same client kind/name before the grace deadline; a matching name alone does not resume claims. A daemon restart starts this grace for still-valid leases, without extending an earlier disconnect deadline.
6. **Bulk starts:** GUI launch and `StartProfile` skip every `tmp-` tunnel, including legacy/person-created ephemerals with launch/profile metadata. An explicit person start may still pin an ephemeral. Agents cannot request launch/profile/favorite metadata, even when saving or promoting.
7. **Crash safety:** leases and ephemeral definitions are written before the spawn. After a daemon restart, expired ones are stopped and removed; the rest continue if the process survived.

### Port allocation

Agents and `dtt forward` may request `auto` for a local port. The engine takes a port from `agent.port_range` (default `20000-20999`) that no tunnel uses and `Listening` reports free, and binds only to `127.0.0.1`. A process can still take the port between the check and the spawn. `ExitOnForwardFailure=yes` already turns that into an exit, so the engine retries with another, previously untried port, up to 3 retries after the initial attempt. Only forward/bind failures trigger allocation retries. An agent-built explicit `bind` that uses any saved tunnel's local/dynamic listen port is refused, even when the saved tunnel is stopped or the bind uses a different local address. Auto allocation already reserves those ports. This is a registry reservation, not an OS-level port reservation; another process may still race the check.

The phase 19 API accepts `forwards[].listen_port` as `"auto"` or a decimal string on `tunnel.runEphemeral`; it is resolved into the existing `bind` field before validation, persistence or spawn. It cannot be combined with `bind`, and is supported only for local/dynamic SSH forwards. Saved definitions retain their existing TOML format. The engine currently takes its range from `engine.Options.PortRange` (default above); settings-to-engine wiring is a later integration step.

## Local API

JSON-RPC 2.0, one message per line, over a Unix socket. Go's `AF_UNIX` is already used for askpass on all three operating systems. gRPC or HTTP would add dependencies and no benefit for one local socket.

- **Location:** `$XDG_RUNTIME_DIR/devt-tunnel/ctl.sock` on Linux when set, otherwise `<state dir>/run/ctl.sock`, in a directory with mode `0700` and an owner check. Socket paths are limited to about 100 bytes, so a longer path falls back to a per-user `0700` directory under the OS temp directory.
- **Authorization:** directory permissions and, on Unix, a peer UID check (`SO_PEERCRED`, `LOCAL_PEERCRED`). On Windows, the per-user ACL of the directory under `%LOCALAPPDATA%`. Whether that is enough on Windows needs testing in phase 12.
- **Handshake:** `daemon.hello {apiVersion, client: {kind, name, version, session?}}` returns `{apiVersion, daemonVersion, pid, session}`. A client retains the opaque session token and supplies it on reconnect; omitting it creates a new session. Overlapping connections with the same token count as one lease holder, and grace begins only when the last closes. `kind` selects the policy and is recorded in the audit log. A client declares it itself, see [Threat model](#threat-model).
- **Version recovery:** an incompatible hello still returns version metadata and permits only `daemon.status` and `daemon.shutdown`; normal client dialing rejects it. `Ensure` uses these recovery methods to implement the upgrade rule.
- **Errors:** JSON-RPC errors carry `data.kind`: `conflict`, `needs_secret`, `keyring_unavailable`, `stopped`, `unknown`, `internal`, `incompatible`, `timeout`, `lease_revoked` (renewing/closing a person-revoked claim), `policy_denied`, or `pending_approval` (with `data.approvalId`). Secret values enter only through `secret.set` or credential-bearing `prompt.answer`; neither returns a value, and request parameters are never logged. `daemon.shutdown` accepts the handshake PID as `expectedPid` and rejects a different daemon identity, so a lost response cannot make a retry stop a replacement process.
- **Types:** one package, `internal/api`, used by every client. New fields are additive, and a breaking change raises `apiVersion`.

| Group | Methods |
|---|---|
| `tunnel.*` | `list` `get` `save` `delete` `start` `stop` `restart` `evaluateDraft` `import` `newDraft` `duplicateDraft` `setFavorite` `promote` `runEphemeral` |
| `lease.*` | `open` `renew` `close` `list` `revoke` (person only) |
| `logs.*` | `read` `since`. Clients poll `since`, as `WatchLogs` polls `LogsSince` today |
| `profile.*` | `list` `save` `delete` `duplicate` `activate` `setAutostart` `start` `stop` |
| `secret.*` | `list` `set` `delete` `rename` `forget`. Values go in only |
| `prompt.*` | `list` `answer` |
| `approval.*` | `list` `get` `decide` (person only) |
| `audit.*` | `read` (person only; filters and sequence cursor) |
| `ssh.*`, `tool.*`, `settings.*`, `orphan.*`, `config.*` | The rest of today's `host.Service`, excluding window and OS auth methods |
| `events.subscribe` | Stream: `tunnels.changed`, `prompt.added`, `approval.added`, `lease.expiring` |
| `daemon.*`, `system.*` | `hello` `status` `shutdown`; `wake` |

Phase 16 also exposes `tunnel.waitReady`, `tunnel.findings`, `tunnel.recordUsed`, `tunnel.startOnLaunch`, `config.overview`, `system.focus` and the current engine settings methods. `internal/api.Methods` is the tested registry. `tunnel.promote`, leases, approvals and audit reads are implemented in phase 19. Each connection handles concurrent requests, cancels their contexts on disconnect, and drops slow event consumers; clients subscribe then resync from `config.overview` after reconnect.

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

For person clients, `preview` still comes from the same builder as the spawn. MCP `tunnel.list`/`tunnel.get` instead return endpoint-only definitions: ID, name, type, state, forwards/endpoints and owner, with basic SSH destination or public-tool address fields. Preview, command definitions (including env references), extra arguments, identity paths, SSH options and secret references are omitted/empty; missing-secret names and tool paths are not returned. Redaction builds a separate view and never changes saved definitions. MCP draft evaluation and saved findings omit secret-presence diagnostics and do not query the keychain. Person inspection and checks remain unchanged.

## Prompts

`SSH_ASKPASS_REQUIRE=force` sends every prompt through the broker, including host-key confirmation and OTP. The daemon queues each one, and the following clients can see it:

- **GUI:** the existing dialog.
- **`dtt start` on a TTY:** asks inline for its own tunnel, like plain `ssh`.
- **`dtt prompts`:** answers from any terminal.
- **MCP:** can see that a prompt is waiting, and its kind, but **cannot answer**. A host-key or password answer from an agent would defeat the prompt's purpose.

Stored SSH passphrases are automatically answered only for the OpenSSH key-file prompt form `Enter passphrase for key '...':`, not arbitrary keyboard-interactive text containing "passphrase for". Other prompts still require a person. This text classification is defense in depth, not proof of a prompt's origin: an SSH server can imitate a key-file prompt.

The 2-minute broker timeout stays. A non-TTY CLI call or an agent gets `needs_user`, and the tunnel fails at the timeout if nobody answers.

## Policy

Policy is per-machine, in `settings.toml`. MCP has no tool to change it. Person clients read the effective defaults through `settings.getAgent {}` and replace the validated `[agent]` policy through `settings.setAgent {agent}`. The wire fields are `enabled`, `public`, `allowTunnels`, `allowSSHHosts`, `portRange`, `defaultTTL` and `maxTTL`. Both methods are outside the MCP allow-list; invalid ranges, durations or allow-lists leave the policy unchanged. Settings → **Agents** edits this policy, and lists all active leases with **Revoke**. Disabling agent mutations does not itself revoke existing leases.

```toml
[agent]
enabled = true
port_range = "20000-20999"
default_ttl = "30m"
max_ttl = "8h"
# The allow-lists below are examples; both default to empty on a new installation.
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

The daemon uses a **constant allow-list**, independent of handler registration and read-only annotations. For a `kind: "mcp"` connection it permits only:

- `daemon.hello` (initial handshake only), `daemon.status`, `events.subscribe`;
- `tunnel.list`, `tunnel.get`, `tunnel.waitReady`, `tunnel.findings`, `tunnel.evaluateDraft`;
- `ssh.hosts`, `ssh.resolve`, `tool.list`, `prompt.list`;
- `lease.open`, `lease.renew`, `lease.close`, `lease.list`, `tunnel.runEphemeral`;
- `approval.list`, `approval.get`, `logs.read`, `logs.since`;
- `tunnel.save`, `tunnel.promote`, both behind approval.

Every other method, including unknown/new methods, returns `policy_denied`. In particular, no `secret.*`, `settings.*`, `profile.*`, `orphan.*`, `config.*`, direct start/stop/restart, prompt answer, tool-path change or shutdown is available. `ssh.resolve` accepts only an alias literally present in the host list for the selected SSH config, rejecting arbitrary manual hosts, wildcard matches and option-shaped aliases. Person resolution is unchanged. Lease renew/close/list are restricted to the caller's actor **and session**, and MCP `lease.open` resolves only daemon-loaded saved definitions, never a client-provided saved classification. `tunnel.save` can add a new definition only (`id` empty, `start` false); promote requires a live lease belonging to the caller. Editing an existing saved definition is refused.

The engine reads `[agent]` from its loaded `settings.toml`. `enabled = false` denies every MCP mutation while keeping inspection/polling available. The configured `default_ttl` and `max_ttl` apply to MCP opens and renewals; person clients retain the ordinary lease defaults. `port_range` supplies the engine's allocator range unless an embedding caller explicitly overrides `Options.PortRange`. Alias allow-list grants require an actual alias in the selected SSH config; a manual hostname disguised as an alias cannot acquire an "always" grant. Public exposure is derived from the actual definition, not a request label.

`checks.AgentSafe` also validates every agent-built tunnel on the daemon side, including a saved tunnel when it is opened. The MCP schema alone is not trusted. A saved `command` tunnel, or a saved tunnel whose options include `ProxyCommand`, `LocalCommand` or `PermitLocalCommand`, always needs approval even when its id matches `allow_tunnels`, because its command runs on open. Aliases in the user's own `ssh_config` are trusted, since the owner wrote them.

Additional admission rules apply to **new MCP-built definitions**, including saves and promotions (not to person-authored saved opens):

- `startOnLaunch: true`, any profile membership, and `favorite: true` are refused with `policy_denied`. Approval fingerprints cover commands, not future launch/profile behavior. A person may later edit a saved definition deliberately.
- Names must be printable UTF-8 without control/format characters or line separators, at most 64 characters. Names are untrusted display data, never part of the daemon-authored approval summary.
- `ssh.passphraseSecret` requires `Host` alone (no manual `Hostname`) and a literal alias in the actual config selected by the built command. Secret names and the destination are quoted in the daemon-authored approval summary. This constrains secret use to owner-authored destinations; approval does not permit an agent to send a vault passphrase to an arbitrary manual host.
- Manual SSH hostnames use only ASCII letters/digits and `._:-`, or a parsed bracketed IPv6 literal. Ngrok `addr`/`domain` and cloudflared `name`/`url`/`hostname` reject leading `-`, whitespace and controls; the extra-argument channel remains denied.
- Forward bind/target values must be a decimal port, one `host:port`, a strictly parsed `[IPv6]:port`, or a Unix socket path starting `/` or `./` with no colon/brackets. Extra colons and arbitrary brackets are rejected. Scoped IPv6 addresses are outside the agent schema. Public classification treats a bind as a Unix socket **only** when it starts `/` or `./` and has no `:`; a slash in a malformed public bind cannot downgrade exposure.
- Saved local listen ports are reserved against agent-chosen explicit binds; each session has at most 32 live leases/ephemerals and 16 pending approvals. These are per-session resource controls, not authentication or global protection against many clients.

### Approvals

A request that needs approval returns `pending_approval` at once. Long-blocking tool calls are not used because clients time them out.

1. The engine records the actor and owning session, client-provided `purpose` (valid UTF-8, at most 1024 bytes), separate `requestedName`, daemon-authored summary, exact argv from `command.Build`, and `Spec.Fingerprint()`. Requested names and purposes are labelled agent-provided/untrusted in the GUI and CLI, are display data rather than instructions, and must not contain secrets. The summary omits the name and quotes the destination and referenced secret names.
2. Person clients inspect/decide through the approval RPCs using the GUI dialog or `dtt approvals` commands. If nobody decides within 5 minutes, it is denied.
3. The choices are **once**, **for this session**, and **always**, which adds the tunnel id or alias to the allow-list. "Always" is offered only when `policy.CanAlwaysAllow` permits it: safe saved IDs or safe loopback alias forwards, never public exposure, unsafe saved execution, manual hosts, new secret references, or save/promote. Saved IDs are escaped as literal allow-list patterns.
4. The agent retries the original operation; approval never starts background execution. The daemon rebuilds the spec and matches the fingerprint, actor, session and operation target. A changed spec needs a new request. "Once" is consumed atomically during admission; "session" can be reused only with the same session token and matching operation/spec. A persistent grant does not bypass the fingerprint check on its first retry.

The concrete RPC flow is nonblocking:

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"a person must approve this request; poll approval.get and retry","data":{"kind":"pending_approval","approvalId":"ap_<32 lowercase hex digits>"}}}
```

`approval.list {}` and `approval.get {id}` expose `id`, actor, purpose, `requestedName`, summary, tunnel/action, fingerprint, `choices`, status, scope and creation/expiry timestamps. Status is `pending`, `approved`, `denied`, `timed_out` or `consumed`. MCP callers can see only their own actor/session's requests; they do **not** receive the code, argv or session token. Person clients additionally see exact argv and the code. `approval.decide {id, decision: "approve" | "deny", scope: "once" | "session" | "always", code}` is person-only; wrong codes, disallowed scopes and decisions on completed/expired requests return `policy_denied`. Approval codes are **eight uppercase hexadecimal characters**, generated with OS randomness. Request IDs use the `ap_` prefix plus 128 random bits. Requests time out at the exact five-minute boundary, using the engine's injectable clock; retrying a timed-out/denied request is denied rather than silently enqueueing it again during its retention period. Each session is limited to 16 pending requests; exceeding that cap returns a clear `policy_denied` error. Denied, timed-out and consumed requests are pruned after one minute even while their session remains connected (or earlier after disconnect). A later retry after pruning is a new request and requires fresh approval. The global queue remains bounded at 1024, including live grants; approved session grants remain available while their session is connected.

`approval.added` carries only `approvalId` and is delivered to subscribed person clients; MCP polls its own requests. MCP prompt listing/events expose the waiting prompt's kind and tunnel, not its free-form text. "Always" writes the literal saved ID or config alias atomically to `settings.toml` and updates the in-memory policy. Pending requests are memory-only; a daemon restart requires fresh approval. Auto-allocated ephemeral definitions retain their chosen ID/port while awaiting a retry. Fingerprint-bound MCP starts do not defer execution until missing secrets arrive and do not silently reallocate a port on forward failure: obtain the missing secret or retry a new definition for a new approval. Person-controlled auto-port retry behavior is unchanged.

The `dtt approvals approve` command requires an interactive terminal and a short code shown by the request. That only slows an agent with shell access; see the next section.

## Threat model

| Threat | Mitigation | Limit |
|---|---|---|
| Other OS users on the machine reach the socket | `0700` directory, peer UID check | Root can always reach it |
| An agent with **only MCP** exposes a port publicly by mistake, or after prompt injection | Approval is always required; the exact argv is shown to a person | A person can approve carelessly |
| An agent with only MCP runs commands through tunnel options | Fields are absent from the schema, and `AgentSafe` rejects them on the daemon | |
| Tunnel output contains a prompt injection | Logs are marked `untrusted` in the tool result and returned only on request | The client decides how to treat them |
| Tunnel output contains a secret | Known secret values are replaced by `[secret:name]` before logs leave the daemon for an MCP client | Secrets the daemon does not know are not caught |
| Forgotten agent tunnels or launch/profile persistence | TTL, connection-drop release, lease caps, rejection of agent launch/profile/favorite metadata; bulk starts skip `tmp-` tunnels | An explicit person start may still pin a tunnel |
| Secrets leaking through argv or API | `secret set` reads stdin or a hidden prompt; MCP saved views omit execution/credential fields; checks do not probe secret presence | Endpoint/name text and unknown/transformed output may still contain sensitive owner-provided data |
| Vault passphrase sent to an agent-chosen SSH server | New passphrase references require a literal owner-authored alias; approval names the destination and secret; askpass accepts only key-file prompt syntax automatically | SSH config and aliased servers remain trusted; a server can imitate key-file prompt text |
| Approval display spoofing through a name | Bounded printable names, daemon-only summary, separate labelled agent-provided name/purpose and exact JSON argv | A person can still believe misleading agent text |
| Malformed forwarding or tool-argument injection | Strict forward parsing, conservative public classification, restricted hostnames and option/whitespace rejection | Owner-written saved execution extensions still need deliberate approval |
| Port squatting or queue/audit flooding | Saved listen-port reservation, per-session lease/pending caps, terminal approval pruning and denial coalescing | Many distinct sessions can bypass per-session caps; CPU/read requests and the global queue are not isolated per OS principal |

**The limit that matters: this is not a security boundary against an agent with a shell.** It runs as the same user, so it can call `dtt` as a "cli" client, edit `settings.toml`, or run `ssh -R` directly. No local design fixes this without a separate OS user or sandbox. The policy protects the MCP route, for agents without shell access or with a client that approves each shell command. It also gives shell-capable agents a tracked option that is easier than a stray `ssh`. Documentation must say this plainly.

## Audit

The daemon opens one `audit.Writer` in the state directory while holding its lifecycle lock. `audit.jsonl` is synced on append and rotates at 1 MiB, keeping the active file plus four archives. Events contain time, actor, action, tunnel/lease/approval IDs, fingerprint, decision, scope, result and an optional numeric coalesced denial `count`; never argv, purpose, summary, arbitrary errors, log content or secret values. The implemented ownership/policy flow records `lease.open`, `lease.renew`, `lease.close`, `lease.expire`, `lease.revoke` (including terminal ephemeral cleanup and daemon shutdown), `tunnel.runEphemeral`, `tunnel.stopEphemeral`, `tunnel.stop` (including person stops that revoke claims), `tunnel.save`, `tunnel.promote`, `approval.request`, `approval.decide`, `approval.timeout`, `policy.update` (persistent-grant admission) and `policy.deny`. Lease/ephemeral admission is audited before any spawn; an audit failure refuses that spawn. Renewals, saves (including person RPC saves) and promotions record audit admission **before** committing expiry/configuration/promotion intent; failure leaves those mutations uncommitted. These are write-ahead admission records, not a transactional proof of successful commit: a later storage failure may leave an audit admission without a corresponding mutation. Audit errors are reported but never prevent lease-release/expiry or person-stop cleanup. Per MCP session, at most 16 individual `policy.deny` records are written per minute. Additional refusals remain denied and are counted in one summary event when the window ends (maintenance or the next call), or on last disconnect/shutdown. Summaries contain only actor and numeric count, not attacker-supplied request text. Counters are memory-only, so a crash can lose an unflushed count. Client actor names accept printable UTF-8 labels, including spaces, up to 256 bytes; control/format characters, line separators and blank labels are rejected at hello and audit validation. Standalone embedded engines can omit the writer; the daemon cannot.

`audit.read {after?, actor?, tunnel?, since?, limit?}` is person-only. `after` is an exclusive persistent sequence cursor; `actor` and `tunnel` are exact filters and `since` is an inclusive RFC3339 timestamp. The response is `{events, next, oldest, lost}`; `next` advances over filtered-out records too, and `lost` warns that rotation discarded history after a nonzero cursor. Responses default to 200 matching records (optional `limit` from 1 to 1000) and are additionally byte-bounded below the RPC message limit; when a page fills, `next` ends at its last returned record so subsequent matches are not skipped. This is the API for the later `dtt audit` command. The audit trail is an owner-readable operational record, not tamper-proof.

For MCP `logs.read` and `logs.since`, the daemon fetches values **only for secret names referenced by that tunnel**. A keychain read failure fails the whole log read closed. Each connection keeps a per-tunnel `redact.Stream`, accepted source cursor, emitted cursor and bounded redacted history. Only emitted records advance the wire `sequence`; a buffered possible secret prefix cannot be skipped by a client-supplied cursor. A retention gap flushes conservatively and starts a new stream; an idle poll after 250 ms flushes an ambiguous suffix as a marker. Successful named-secret reads are observed before values reach process environments or askpass. The daemon retains the union of those values for the lifetime of each raw log ring, including former definition references, rotations, promotions and reconnects; no unrelated vault entries are fetched. Named-value changes rebuild connection-local redaction from retained input. Immutable matcher identity distinguishes a deleted/recreated tunnel ID from an older ring, preventing stale cursor/matcher reuse. Matcher and raw-log snapshots are taken together with secret delivery serialized, so a new value cannot enter the captured output before its matcher. Person clients keep raw logs. Status errors (which can contain process output) are replaced by a generic message for MCP inspection/wait calls, preventing a raw-log side channel.

Redaction catches byte-exact known values, including values split across writes/records, and emits `[secret:name]`. Values shorter than four bytes, unknown/transformed values, input lost to retention and matches spanning an idle flush are not protected. An idle flush may over-redact innocent prefixes. Redacted output remains untrusted data.

## GUI changes

Small, and using existing screens:

- Tunnel rows and detail panel: an agent badge with the holder name and live time left (including disconnect grace), and an `ephemeral` tag with **Save**. Save asks for a name and uses `tunnel.promote`, without restarting the process. Mixed person/agent claims still show the agent badge.
- Approval dialog, built on the SSH prompt dialog, showing actor, separately labelled agent-provided requested name and purpose, daemon-authored summary, exact argv as a JSON array, request code and expiry countdown. Requests queue in daemon order. The GUI resyncs on startup/reconnect and `approval.added`, and polls to observe decisions from other person clients and expiry. Native windows are brought forward for new/recovered requests when desktop integration is available. A deliberate button click sends the request's code verbatim to `approval.decide`; no code retyping is required in the GUI. Only the daemon's allowed scopes are offered, so public exposure has no **Always**. Approving grants a retry, not background execution.
- Settings: an **Agents** section for policy and active leases, including detached person leases with no expiry. **Install command-line tool** remains future packaging work.
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
