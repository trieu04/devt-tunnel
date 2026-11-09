## devt-tunnel approvals approve

Approve a pending agent request

### Synopsis

Approve a pending fingerprint-bound agent request. Requires a TTY, even with --json, and asks you to type the code displayed in devt-tunnel approvals. Codes are read from the terminal, never flags or piped stdin; --no-input exits 4. --scope must be one of the request's allowed scopes. The agent must retry after approval; no command is run by this decision. Also available as devt-tunnel approve.

```
devt-tunnel approvals approve <id> [flags]
```

### Examples

```
  devt-tunnel approvals approve ap_123
  devt-tunnel approve ap_123 --scope session
```

### Options

```
  -h, --help           help for approve
      --scope string   Permission scope: once, session or always (must be allowed by the request) (default "once")
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

* [devt-tunnel approvals](devt-tunnel_approvals.md)	 - List pending agent requests for a person

