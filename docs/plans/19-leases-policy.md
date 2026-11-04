# Phase 19: Ownership, leases and policy

**Goal:** every tunnel has an owner, temporary tunnels clean themselves up, and agent requests pass through one enforced policy.

**Depends on:** 16, 17. Design: [control-plane.md](../design/control-plane.md#tunnel-ownership).

## 19.1 Ephemeral tunnels

- `runtime.json` in the state directory holds ephemeral definitions and leases, written atomically before any spawn.
- The engine always gives `Manager.SetConfig` saved plus ephemeral definitions, because `SetConfig` stops any tunnel missing from the config. Add a regression test: saving `tunnels.toml` leaves ephemeral tunnels running.
- `tunnels.toml` writes must never include ephemeral tunnels, and validation runs on the merged set so that ids cannot collide. Saved ids may not start with `tmp-`.
- `tunnel.promote` moves a definition to `tunnels.toml` without restarting the process. Check that `Stale` stays false.

## 19.2 Leases

Implement the [rules](../design/control-plane.md#rules) in `internal/engine`, with a clock passed in for tests.

| Case | Expected |
|---|---|
| Two leases on one tunnel; one closes | Keeps running |
| Last lease closes, lease started it, not pinned | Stops |
| Tunnel was already running | Keeps running |
| A person started it | Pinned; no expiry stops it |
| A person stops it | Stops at once; leases become `lease_revoked` |
| TTL passes | Lease ends |
| Connection drops | Ends after the 2-minute grace unless the client reconnects |
| Daemon restart | Expired removed; surviving processes reattached as orphans or stopped, per OS |

## 19.3 Port allocation

`listen_port: auto` picks from `agent.port_range`, checks `Listening` and other tunnels, and binds `127.0.0.1`. It retries up to 3 times on forward failure, which `ExitOnForwardFailure=yes` turns into an exit.

## 19.4 Policy

- Add the `[agent]` section to `settings.toml`, with the defaults in the design.
- `checks.AgentSafe(t model.Tunnel) []Finding` rejects `command` tunnels and any `SSH.Flags`, `SSH.Options`, `SSH.ExtraArgs` or `Identity` outside the builder's defaults. For `SSH.Options` and `ExtraArgs`, match case-insensitively and catch `-oProxyCommand=...` and `-o ProxyCommand ...`, plus `LocalCommand`, `PermitLocalCommand` and `-F`. Extra args on ngrok and cloudflared are rejected too.
- `policy.Decide(actor, action, tunnel) (allow|ask|deny, reason)` is a pure function with one test per row of the design's table.

The checks run in the daemon for every `kind: "mcp"` caller, not only in the MCP schema.

## 19.5 Approvals

- A queue in the engine stores actor, purpose, summary, argv and `Spec.Fingerprint()`. Requests time out as denied after 5 minutes.
- On approval, run only that fingerprint. A test changes the spec after approval and expects a new request.
- GUI: a dialog based on `PromptDialog.tsx`. "Always" is not offered for public exposure.
- CLI: `dtt approvals`. `approve` requires a TTY and the code shown in the request.

## 19.6 Log redaction

Before logs go to an MCP client, replace known secret values with `[secret:name]`. Include a test where a value is split across two writes, and record what is not caught. Values come from the keychain only for tunnels that name them.

## 19.7 Audit

`audit.jsonl`, rotating. A test checks that a full open, approve and close flow writes no secret value and no log text.

## 19.8 GUI

Agent badge with holder and time left, `ephemeral` tag with **Save**, and an **Agents** section in Settings. Bound types change, so regenerate bindings.

## Exit test

Using the socket directly with `kind: "mcp"`:

- Each row of the policy table behaves as designed.
- A `ProxyCommand` request is refused.
- A public tunnel waits until a person approves in the native window.
- Killing the client ends its lease after the grace period, and the process is gone.
- A tunnel that a person pinned is untouched.
