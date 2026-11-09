## devt-tunnel approvals

List pending agent requests for a person

### Synopsis

Show pending fingerprint-bound agent requests with their summary, exact argv, actor, purpose, allowed scopes, expiry and confirmation code. Codes are intentionally visible to person clients, never to agents. Approval does not run anything: the requesting agent must retry. Use approvals approve/deny, or their top-level approve/deny aliases.

```
devt-tunnel approvals [flags]
```

### Examples

```
  devt-tunnel approvals
  devt-tunnel approvals --json
  devt-tunnel approvals approve ap_123 --scope once
```

### Options

```
  -h, --help   help for approvals
```

### Options inherited from parent commands

```
      --json               Write versioned JSON results and errors
      --no-color           Disable color (also NO_COLOR)
      --no-daemon-start    Do not start the daemon automatically
      --no-input           Never prompt for input
  -q, --quiet              Suppress informational diagnostics
      --timeout duration   Daemon connection and request timeout (default 10s)
```

### SEE ALSO

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels
* [devt-tunnel approvals approve](devt-tunnel_approvals_approve.md)	 - Approve a pending agent request
* [devt-tunnel approvals deny](devt-tunnel_approvals_deny.md)	 - Deny a pending agent request

