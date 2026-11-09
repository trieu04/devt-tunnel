# Central tunnel management

Status: implemented. Open acceptance is in the [roadmap](../roadmap.md). Companion contracts: [cli.md](cli.md) and [mcp.md](mcp.md).

## Problem and scope

Tunnels live in terminals, in the desktop app and in background processes started by agents, and nothing answers "what is open on this machine, who opened it, and when does it close?".

Goals:

- One registry and one supervisor per user per machine, shared by the app, the CLI and agents.
- Every agent-opened tunnel has an owner and an expiry, and is cleaned up without human help.
- Agents without a shell get an MCP channel with limits they cannot change through MCP.

Non-goals: team management or accounts; isolating a hostile agent running as the same OS user (see [Threat model](#threat-model)); replacing `ssh`, `ngrok` or `cloudflared`; interactive shells.

## Design

```text
 Desktop app             dtt (CLI)            Agent client
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

`tunnels.toml` stays the source of saved definitions. The daemon is its only writer in normal use; `config.Store` detects hand edits and returns `ErrConflict`.

## Why a daemon

The CLI and agents need one registry shared with the GUI. Hosting the API in the GUI would need a display and could open an app on every CLI call. Letting whichever client starts first host the engine would create two code paths for every GUI call. An always-on-demand daemon with the GUI as a client leaves one path to test.

### Daemon lifecycle

- **Binary:** `dtt daemon run`, from one Go binary without Wails, built with `CGO_ENABLED=0`. Packages ship it next to the app and `devt-tunnel-askpass`.
- **Start:** any client starts it on demand, detached from the caller. `--no-daemon-start` turns this off.
- **Single instance:** an advisory lock on `daemon.lock` decides ownership, since a stale socket file can survive a crash.
- **Logs:** its own `daemon.log`.
- **Idle exit:** after 5 minutes with no active tunnel, lease, pending prompt or connected client.
- **Upgrade:** a matching API and build is reused. A different build with the same API stays while active and is replaced when idle. A different API is replaced only when idle; otherwise the error names `dtt daemon restart`, which stops all tunnels.
- **Crash:** on Unix, `processes.json` and `FindOrphans` find surviving children. On Windows, `KILL_ON_JOB_CLOSE` ends them with the daemon, and leases are restored as stopped.

### Behavior across clients

- ⌘Q calls `daemon.shutdown` after a dialog naming any agent leases. Closing the window with a tray only hides it.
- The daemon starts nothing by itself. The GUI requests `StartOnLaunch` starts once at launch.
- The daemon monitors clock jumps; the GUI forwards OS wake events with `system.wake` and calls `secrets.forget` on focus.
- Reveal stays in the GUI: authenticate, then read the keychain directly. The API has no method that returns a value.
- All `settings.toml` writes go through the daemon.
- The engine always passes saved plus ephemeral definitions to `Manager.SetConfig`, so a save cannot kill an ephemeral tunnel.

## Tunnel ownership

| Concept | Meaning | Stored in |
|---|---|---|
| Saved tunnel | Shareable definition | `tunnels.toml` |
| Ephemeral tunnel | Created by `dtt run`, `forward`, `expose` or an agent. Id starts with `tmp-`. Removed when it stops. `dtt save` or `tunnel_save` promotes it in place, keeping the process | `runtime.json` |
| Lease | A claim by one client on one tunnel until a set time | `runtime.json` |

`runtime.json` is written atomically with mode `0600` before any spawn. Person-revoked leases stay as tombstones until their original expiry, so the next renew or close returns `lease_revoked`.

### Rules

1. **A person starting a tunnel pins it.** No lease expiry stops a pinned tunnel.
2. **Opening adds a lease and starts the tunnel if needed.** Clients can share a running tunnel, each with its own lease.
3. **Closing or expiring removes a lease.** The tunnel stops when no leases remain, it is not pinned, and a lease started it.
4. **A person always wins.** `dtt stop` and the GUI stop the tunnel at once and revoke its leases. An agent can only release its own lease.
5. **Disconnect:** when a lease holder's last connection drops, its leases end after the grace period. Reconnecting needs the same `client.session` token and client kind and name. A daemon restart starts this grace for valid leases.
6. **Bulk starts** (GUI launch, `StartGroup`) skip every `tmp-` tunnel. Agents cannot request launch, profile or favorite metadata.
7. **Crash safety:** after a daemon restart, expired leases and ephemerals are stopped and removed; the rest continue if the process survived. Surviving Unix children stay in the orphan list and are not respawned.

### Limits

| Limit | Value |
|---|---|
| Lease TTL | Default 30 minutes, maximum 8 hours, renewable up to the maximum per renewal |
| Disconnect grace, SSH prompt timeout | 2 minutes |
| Approval timeout | 5 minutes |
| Per MCP session | 32 live leases, 16 pending approvals, 64 open-request records |
| Global | 1024 approvals, 1024 open-request records |
| Retention of finished approvals and requests | 1 minute |
| Audit | 1 MiB per file, four archives; 16 `policy.deny` records per session per minute, the rest counted in a summary |

Per-session caps are resource controls, not protection against many clients.

### Port allocation

`auto` takes a port from `agent.port_range` (default `20000-20999`) that no tunnel uses and that is free, and binds only to `127.0.0.1`. If another process takes it first, `ExitOnForwardFailure=yes` ends the attempt and the engine retries with a new port, up to 3 times. Agent-chosen explicit binds cannot use any saved tunnel's listen port, even when that tunnel is stopped. This is a registry reservation, not an OS one.

## Local API

JSON-RPC 2.0, one message per line, over a Unix socket. `internal/api` holds the shared types and `internal/api.Methods` is the tested method registry. New fields are additive; a breaking change raises `apiVersion`.

- **Location:** `$XDG_RUNTIME_DIR/devt-tunnel/ctl.sock` on Linux when set, otherwise `<state dir>/run/ctl.sock`, in a `0700` directory. Paths over about 100 bytes fall back to a per-user directory under the OS temp directory.
- **Authorization:** directory permissions plus a peer UID check on Unix; the per-user directory ACL on Windows, which still needs native testing.
- **Handshake:** `daemon.hello {apiVersion, client: {kind, name, version, session?}}` returns `{apiVersion, daemonVersion, pid, session}`. `kind` selects the policy and is self-declared, see [Threat model](#threat-model). An incompatible hello permits only `daemon.status` and `daemon.shutdown`.
- **Errors:** JSON-RPC errors carry `data.kind`, defined in `internal/api`.
- **Secrets:** values enter only through `secret.set` or a credential-bearing `prompt.answer`. Neither returns a value, and request parameters are never logged.

| Group | Methods |
|---|---|
| `tunnel.*` | `list` `get` `save` `delete` `start` `stop` `restart` `evaluateDraft` `import` `newDraft` `duplicateDraft` `setFavorite` `promote` `runEphemeral` `waitReady` `findings` |
| `lease.*` | `open` `renew` `close` `list` `revoke` (person only) |
| `logs.*` | `read` `since` |
| `profile.*`, `group.*` | CRUD; `group.start` `group.stop` |
| `secret.*` | `list` `set` `delete` `rename` `forget` |
| `prompt.*` | `list` `answer` |
| `approval.*` | `list` `get` `decide` (person only) |
| `audit.*` | `read` (person only) |
| `ssh.*`, `tool.*`, `settings.*`, `orphan.*`, `config.*` | The rest of `host.Service` |
| `events.subscribe` | `tunnels.changed`, `prompt.added`, `approval.added`, `lease.expiring`. Clients resync from `config.overview` after reconnect |
| `daemon.*`, `system.*` | `hello` `status` `shutdown`; `wake` `focus` |

**Endpoints** are always an array, of kind `local`, `remote`, `socks` or `public`. `public` endpoints have an empty `listen` until the address is known. The `public` field describes the bind; agent policy still requires approval for every remote forward.

**MCP views** are endpoint-only: id, name, type, state, forwards, owner and basic destination fields. Preview, command definitions, extra arguments, identity paths, SSH options, secret references and secret-presence diagnostics are omitted. For person clients, `preview` comes from the same builder as the spawn.

## Prompts

`SSH_ASKPASS_REQUIRE=force` sends every prompt through the broker, including host keys and OTP. The GUI shows a dialog, `dtt start` on a TTY asks inline for its own tunnel, and `dtt prompts` answers from any terminal. MCP can see a prompt's kind but **cannot answer**. A non-TTY CLI call or an agent gets `needs_user`, and the tunnel fails at the timeout if nobody answers.

Stored passphrases are answered automatically only for the OpenSSH form `Enter passphrase for key '...':`. This is defense in depth: an SSH server can imitate that text.

## Policy

Policy is per-machine, in `settings.toml`, edited in Settings → **Agents** or through `settings.setAgent` by person clients. MCP has only the read-only `policy.get`, limited to non-sensitive fields. Invalid values leave the policy unchanged. Disabling agent access does not revoke existing leases.

```toml
[agent]
enabled = true
port_range = "20000-20999"
default_ttl = "30m"
max_ttl = "8h"
# Both allow-lists default to empty.
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

**MCP allow-list.** The daemon permits a `kind: "mcp"` connection only these methods; anything else, including new methods, returns `policy_denied`:

- `daemon.hello`, `daemon.status`, `events.subscribe`, `policy.get`
- `tunnel.list` `get` `waitReady` `findings` `evaluateDraft` `runEphemeral` `openRequest` `waitRequest` `checkRequest`
- `ssh.hosts`, `ssh.resolve` (literal aliases only), `tool.list`, `prompt.list`, `logs.read`, `logs.since`
- `lease.open` `renew` `close` `list`, restricted to the caller's actor and session
- `approval.list`, `approval.get`
- `tunnel.save` (new definitions only) and `tunnel.promote` (needs the caller's live lease), both behind approval

With `enabled = false`, every MCP method returns `agent_disabled` except `daemon.hello`, `daemon.status` and `policy.get`, and event delivery stops.

**Daemon-side checks.** `checks.AgentSafe` validates every agent-built tunnel; the MCP schema alone is not trusted. Public exposure is derived from the actual definition. A saved `command` tunnel, or one with `ProxyCommand`, `LocalCommand` or `PermitLocalCommand`, always needs approval even when allow-listed. Aliases in the user's own `ssh_config` are trusted.

New MCP-built definitions, including saves and promotions, must also satisfy:

- No `startOnLaunch`, profile, group or `favorite`.
- Names are printable UTF-8, at most 64 characters, and are never part of the daemon-authored summary.
- `ssh.passphraseSecret` needs a literal alias from the selected SSH config and no manual hostname.
- Manual hostnames use only ASCII letters, digits and `._:-`, or a bracketed IPv6 literal. Ngrok and cloudflared fields reject leading `-`, whitespace and controls.
- Forward values must be a decimal port, one `host:port`, a strict `[IPv6]:port`, or a Unix socket path starting `/` or `./` with no colon or brackets. Anything else is rejected, so a malformed bind cannot downgrade public classification.

### Approvals

A request that needs approval returns `pending_approval` at once with `data.approvalId`; long-blocking calls are avoided because clients time them out.

1. The engine records the actor, session, `purpose` (at most 1024 bytes), requested name, daemon-authored summary, exact argv and `Spec.Fingerprint()`. Agent-provided text is labelled untrusted wherever it is shown.
2. A person decides in the GUI or with `dtt approvals`. Undecided requests are denied at the timeout. `approval.decide` needs a code of eight uppercase hex characters, which MCP callers never receive; they also do not get argv or the session token.
3. The scopes are **once**, **session** and **always**. `policy.CanAlwaysAllow` offers "always" only for safe saved ids or safe loopback alias forwards, never for public exposure, unsafe saved execution, manual hosts, new secret references, or save and promote.
4. The agent retries the original operation; approval never starts background execution. The daemon rebuilds the spec and matches fingerprint, actor, session and target. A changed spec needs a new request.

Pending requests are memory-only, so a daemon restart requires fresh approval. Retrying a denied or timed-out request is denied until it is pruned. Fingerprint-bound MCP starts do not wait for missing secrets or reallocate a port on failure.

The terminal code only slows an agent with shell access; see below.

## Threat model

| Threat | Mitigation | Limit |
|---|---|---|
| Other OS users reach the socket | `0700` directory, peer UID check | Root can always reach it |
| An MCP-only agent exposes a port by mistake or after prompt injection | Approval is required and shows exact argv | A person can approve carelessly |
| An MCP-only agent runs commands through tunnel options | Fields are absent from the schema, and `AgentSafe` rejects them | |
| Tunnel output contains a prompt injection | Logs are marked `untrusted` and returned only on request | The client decides how to treat them |
| Tunnel output contains a secret | Known values become `[secret:name]` before reaching MCP | Unknown or transformed values are not caught |
| Forgotten agent tunnels | TTL, disconnect release, lease caps, no launch metadata | An explicit person start may still pin one |
| Secrets leak through argv or API | Hidden prompt or stdin only; MCP views omit credential fields | Owner-written names and endpoints may be sensitive |
| Vault passphrase sent to an agent-chosen server | Literal owner-authored alias only; approval names destination and secret | A trusted server can imitate key-file prompt text |
| Approval display spoofing | Bounded names, daemon-only summary, labelled agent text, exact argv | A person can still believe misleading text |
| Malformed forwards or argument injection | Strict parsing and conservative public classification | Owner-written saved extensions still need deliberate approval |
| Port squatting or queue flooding | Saved-port reservation, per-session caps, pruning, denial coalescing | Many distinct sessions can bypass per-session caps |

**This is not a security boundary against an agent with a shell.** It runs as the same user, so it can call `dtt` as a "cli" client, edit `settings.toml`, or run `ssh -R` directly. The policy protects the MCP route, and gives shell-capable agents a tracked option that is easier than a stray `ssh`. Documentation must say this plainly.

## Audit and log redaction

`audit.jsonl` in the state directory is synced on append. Events hold time, actor, action, ids, fingerprint, decision, scope and result; never argv, purpose, summary, log content or secret values.

- Lease, ephemeral, renewal, save and promotion admission is audited first, and an audit failure refuses the operation. These records are write-ahead, so a later storage failure can leave one without a matching change.
- Audit errors never block lease release, expiry or a person's stop.
- `audit.read` is person-only and used by `dtt audit`. The trail is an owner-readable record, not tamper-proof.

For MCP `logs.*`, the daemon fetches only the secrets that tunnel references, and a keychain failure fails the read. Redaction catches byte-exact known values, including values split across records. Values shorter than four bytes, transformed values, and matches spanning an idle flush are not protected. Person clients keep raw logs. Status errors, which can contain process output, are replaced with a generic message for MCP.

## GUI surface

- Tunnel rows and detail: an agent badge with holder and time left, and an `ephemeral` tag with **Save**.
- Approval dialog: actor, labelled agent-provided name and purpose, daemon summary, exact argv, code and countdown. A button click sends the code; only allowed scopes are offered.
- Settings → **Agents**: policy and active leases with **Revoke**. On macOS, **Install command-line tool** links `dtt` onto `PATH`.
- A banner and retry when the daemon cannot be reached.

## Risks

- **Keychain from a background process** is unverified on macOS, and Secret Service without a desktop needs acceptance. Affected tunnels report `keyring_unavailable`.
- **SSH agent:** the daemon keeps its launch `SSH_AUTH_SOCK` and does not replace it. Restart it from the desired session if that path goes stale.
- **macOS signing:** a second signed executable launched detached needs checking under hardened runtime and notarization.
- **Tunnels with no window:** the daemon allows them for CLI use. `dtt ls`, `dtt daemon status` and the GUI show everything.
- **MCP changes quickly:** keep it in a thin adapter so SDK upgrades do not reach the engine.

## Decisions

Accepted by the project owner on 2026-11-04.

| # | Decision | Consequence |
|---|---|---|
| C1 | An on-demand daemon replaces "one process, no daemon" | The GUI, CLI and MCP are all clients |
| C2 | "Central" means per user, per machine | No remote transport yet; a headless server is a [later stage](../roadmap.md#later-headless-servers), and a team product would need its own design |
| C3 | ⌘Q keeps stopping everything, including CLI and agent tunnels | The tray could later add "Close app, keep tunnels" |
| C4 | Public exposure by an agent always needs a person | Unattended agents cannot publish local services |
| C5 | The CLI is named `dtt` and uses `cobra` | `devt-tunnel` remains the app |
