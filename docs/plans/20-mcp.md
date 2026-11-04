# Phase 20: MCP server

**Goal:** all of [mcp.md](../design/mcp.md). An agent with no shell can create and use tunnels within policy.

**Depends on:** 19.

## 20.1 Adapter

`internal/mcpserver`, run by `dtt mcp serve`, using the official Go SDK at the current release (v1.8.0 on 2026-11-04). Recheck its supported protocol revisions and migration notes first.

The adapter maps tools to API calls and holds no state or policy, so an SDK upgrade cannot reach the engine. Nothing may write to stdout except the protocol. Send diagnostics to stderr.

## 20.2 Tools

Implement in this order, since each can be tested alone:

1. Read: `tunnel_list`, `tunnel_get`, `tunnel_logs`, `ssh_host_list`, `tunnel_check`.
2. Open: `tunnel_open`, `tunnel_wait`, with `request_id` idempotency stored in the engine.
3. Lifetime: `tunnel_renew`, `tunnel_close`.
4. Keep: `tunnel_save`.

Each has an input schema, an `outputSchema`, annotations, and the text in mcp.md.

## 20.3 Resources and setup

- `devt-tunnel://docs/agent-guide` from an embedded `docs/cli/agents.md`, and `devt-tunnel://policy`.
- The server `instructions` text.
- `dtt mcp config` prints configuration. `dtt mcp tools` prints the tool list for review.

## Tests

As listed in [mcp.md](../design/mcp.md#testing): contract, schema guard, policy table through MCP, and lifecycle with the private `sshd`.

## Real client check

Automated tests with our own client cannot show that other clients work. Run scripted checks with at least Claude Code, and one client on an older protocol revision if we claim support for it. Record client versions and results in `docs/cli/agents.md`.

## Exit test

A real MCP client with no shell tool opens a forward to an allow-listed alias, connects through it and closes it, and the process is gone afterward. The same client asking for a public tunnel is held until a person approves. A denial is reported without a workaround being attempted through MCP.
