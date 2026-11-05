## dtt deny

Deny a pending agent request

### Synopsis

Deny an agent request without granting permission. No terminal or confirmation code input is needed to deny. The request is fingerprint-bound, and deciding it never starts a tunnel. This command is also available as dtt deny.

```
dtt deny <id> [flags]
```

### Examples

```
  dtt approvals deny ap_123
  dtt deny ap_123 --json
```

### Options

```
  -h, --help   help for deny
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

