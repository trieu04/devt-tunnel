## dtt approvals

List pending agent requests for a person

### Synopsis

Show pending fingerprint-bound agent requests with their summary, exact argv, actor, purpose, allowed scopes, expiry and confirmation code. Codes are intentionally visible to person clients, never to agents. Approval does not run anything: the requesting agent must retry. Use approvals approve/deny, or their top-level approve/deny aliases.

```
dtt approvals [flags]
```

### Examples

```
  dtt approvals
  dtt approvals --json
  dtt approvals approve ap_123 --scope once
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt approvals approve](dtt_approvals_approve.md)	 - Approve a pending agent request
* [dtt approvals deny](dtt_approvals_deny.md)	 - Deny a pending agent request

