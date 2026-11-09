## devt-tunnel leases revoke

Revoke a lease and stop its unowned tunnel

### Synopsis

Revoke a client's claim immediately. If no other lease or person pin keeps the tunnel running, its process tree is stopped. Replaces killing an agent's tunnel process directly; the agent receives lease_revoked on its next operation.

```
devt-tunnel leases revoke <id> [flags]
```

### Examples

```
  devt-tunnel leases revoke ls_123
  devt-tunnel leases revoke ls_123 --json
```

### Options

```
  -h, --help   help for revoke
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

* [devt-tunnel leases](devt-tunnel_leases.md)	 - List clients holding tunnels and their expiries

