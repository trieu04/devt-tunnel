# MCP server for agents

Status: implemented with automated checks; real-client acceptance is on the [roadmap](../roadmap.md). Ownership, leases, policy and the threat model are in [control-plane.md](control-plane.md). Setup and usage are in the [agent guide](../cli/agents.md).

## Overview

`dtt mcp serve` is a thin adapter: MCP over stdio on one side, the daemon's local API on the other. It holds no tunnel state, makes no policy decisions, and starts the daemon if needed. Each agent client spawns its own copy.

```text
Agent client ── stdio ── dtt mcp serve ── local socket ── dtt daemon ── ssh · ngrok · cloudflared
```

- **Protocol:** the official Go SDK, with its default protocol revisions. Those are negotiation capabilities, not real-client compatibility claims.
- **Transport:** stdio only. Streamable HTTP would add a network listener and token handling for a case that is out of scope.
- **State:** `lease_id` and `request_id` are explicit in calls, so the contract does not depend on an MCP session. The adapter connects with `kind: "mcp"` and a printable name of at most 64 characters, from MCP initialize or `--name`, and resends its session token on reconnect.

## Tools

`internal/mcpserver/tools.golden.json` freezes tool names, descriptions, schemas and annotations. Regenerate it only after review, with `UPDATE_GOLDEN=1 go test ./internal/mcpserver -run TestToolContractGolden`.

All tools return `structuredContent` matching a declared `outputSchema`, plus a short text summary.

| Tool | Input | Output |
|---|---|---|
| `tunnel_list` | `state?`, `type?` | `{ tunnels: Tunnel[] }` |
| `tunnel_get` | `tunnel_id` | `Tunnel`, `preview`, `findings`, `your_leases` |
| `tunnel_logs` | `tunnel_id`, `lines?` (default 50, max 500), `after_seq?` | `{ untrusted, note, lines: [{seq, text}], next_seq }` |
| `ssh_host_list` | `profile?` | `{ hosts: [{alias, has_tunnel, allowed}] }` |
| `tunnel_check` | Same as `tunnel_open`; `request_id` and `purpose` optional | `{ decision, reason, preview, findings }` |
| `tunnel_open` | See below | `OpenResult` |
| `tunnel_wait` | `request_id`, `wait_seconds?` (0 to 30, default 20) | `OpenResult` |
| `tunnel_renew` | `lease_id`, `ttl_seconds?` | `{ lease_id, expires_at }` |
| `tunnel_close` | `lease_id` | `{ tunnel_state, stopped, reason }` |
| `tunnel_save` | `tunnel_id`, `name` | `OpenResult`-style, with `approval_id` or `saved_id` |

Contract notes:

- `tunnel_wait` is deliberately not read-only: approvals authorize a later retry, so a poll can start the approved process.
- MCP views are redacted by the daemon: `tunnel_get` returns an empty `preview` and `missing_secrets`, and there is no profile filter. `ssh_host_list` returns aliases only.
- `usable_without_approval` is false for temporary ids. Reuse a temporary tunnel by retrying its original `request_id`.
- Pass `next_seq` unchanged as `after_seq`.
- `tunnel_close` is idempotent. With no owned lease left it returns `tunnel_state: "unknown", stopped: false`. `reason` says why a tunnel kept running, under the [lease rules](control-plane.md#rules).
- `tunnel_save` has no `request_id` and takes no profile, launch or favorite metadata. It always needs approval; retry the identical call afterwards, not `tunnel_wait`.

### `tunnel_open` input

Exactly one of `tunnel_id` and `spec`. `request_id` and `purpose` are required. `ttl_seconds` defaults and is capped by policy; `wait_seconds` is 0 to 30.

`spec` is one of the following. **There is deliberately no way to pass flags, `-o` options, extra arguments, an identity path or a command.**

```jsonc
// SSH through a config alias, or a manual hostname (always needs approval)
{ "type": "ssh", "host": "staging",
  "forwards": [
    { "kind": "L", "listen_port": "auto", "target": "127.0.0.1:5432" },
    { "kind": "D", "listen_port": "auto" },
    { "kind": "R", "remote_bind": "127.0.0.1:8080", "local_target": "127.0.0.1:3000" }
  ] }

// Public: always needs approval
{ "type": "cloudflared_quick", "local_url": "http://127.0.0.1:3000" }
{ "type": "ngrok", "proto": "http", "local_addr": "127.0.0.1:3000", "domain": "x.ngrok.app", "secret_name": "ngrok-personal" }
```

Local listeners always bind `127.0.0.1`. An SSH `spec` always runs with `-N` and `ServerAliveInterval=30`.

### `OpenResult`

`status` is `ready`, `starting`, `pending_approval`, `needs_user`, `denied`, `expired` or `failed`, and every result echoes `request_id`.

```jsonc
{ "status": "ready", "request_id": "6f1c...", "lease_id": "ls_9a2", "tunnel_id": "tmp-staging-1",
  "expires_at": "2026-11-04T11:30:00Z",
  "endpoints": [{ "kind": "local", "listen": "127.0.0.1:20437", "target": "127.0.0.1:5432", "public": false }] }

{ "status": "pending_approval", "request_id": "6f1c...", "approval_id": "ap_123", "expires_at": "2026-11-04T11:05:00Z",
  "summary": "Publish http://127.0.0.1:3000 to the internet through cloudflared",
  "message": "A person must approve this in devt-tunnel. Tell them, then call tunnel_wait." }
```

`needs_user` carries `prompt_kind` and a message; prompt text is never returned.

### Request tracking

The engine owns open-request state through `tunnel.openRequest` and `tunnel.waitRequest`; `tunnel.checkRequest` gives a dry-run decision. Request ids are scoped to actor and session, retries are serialized, and changed arguments are refused. The record cap refuses a new entry rather than evicting a live id. Idempotency holds only during retention, and a daemon restart loses records and requires fresh approval.

## Shared types

```jsonc
// Tunnel
{ "id": "dev-db", "name": "Dev DB", "type": "ssh", "state": "running",
  "error": "",
  "endpoints": [{ "kind": "local", "listen": "127.0.0.1:15432", "target": "127.0.0.1:5432", "public": false }],
  "owner": { "pinned": true, "ephemeral": false, "leases": 1 },
  "usable_without_approval": true,
  "missing_secrets": [] }
```

`state` is `stopped`, `starting`, `running`, `reconnecting`, `failed`, `needs_token` or `keyring_unavailable`. `endpoints[].kind` is `local`, `remote`, `socks` or `public`.

## Errors

Business outcomes such as `denied` or an asynchronous start failure are normal results. Failures return `isError: true` with `structuredContent`:

```json
{ "code": "port_in_use", "message": "127.0.0.1:5433 is in use by tunnel \"db\"", "hint": "Use listen_port \"auto\", or use tunnel \"db\" if it fits.", "retryable": false }
```

| Code | What the agent should do |
|---|---|
| `unknown_tunnel` | Call `tunnel_list` |
| `invalid_spec` | Fix the input |
| `port_in_use` | Use `auto` |
| `policy_denied`, `agent_disabled`, `keyring_unavailable`, `tool_not_found` | Tell the person. Do not work around it |
| `needs_secret` | Tell the person the secret's name |
| `lease_expired`, `lease_revoked` | Open again only if the person still wants it |
| `daemon_unavailable` | Retry once, then tell the person |

## Resources and guidance

| URI | Content |
|---|---|
| `devt-tunnel://docs/agent-guide` | Same text as `docs/cli/agents.md` |
| `devt-tunnel://policy` | Effective policy from `policy.get`, read-only and available while agents are disabled |

No MCP prompts in v1. Status changes go through `tunnel_wait`, because not all clients pass notifications to the model. The server's `instructions` text is `Instructions` in `internal/mcpserver/server.go`; for an agent with shell access nothing enforces it, as the [threat model](control-plane.md#threat-model) explains.

## Testing

- **Contract:** an in-memory MCP client calls each tool against a real daemon on a temporary socket.
- **Schema:** a test fails if any tool input gains a property named like `flags`, `options`, `extra_args`, `identity`, `command` or `line`.
- **Policy table:** one test per row, through MCP and directly to the socket with `kind: "mcp"`.
- **Stdout purity:** stdout must decode as JSON-RPC only, with and without `--json`.
- **Lifecycle** with the private `sshd` in `internal/e2e`: open, connect, close, expiry, adapter kill, pinned survival, shared leases and `request_id` retry.
- **Real client:** one scripted run before each release that changes the SDK or a tool.
