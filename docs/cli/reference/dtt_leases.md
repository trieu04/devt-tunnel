## dtt leases

List clients holding tunnels and their expiries

### Synopsis

Inspect the claims keeping tunnels running, rather than guessing from process IDs. Shows lease ID, tunnel, holder, effective expiry, whether it started the tunnel and whether it is detached. Detached person leases survive CLI exit; an expiry of never means explicit stop or revoke is required.

```
dtt leases [flags]
```

### Examples

```
  dtt leases
  dtt leases --json
  dtt leases revoke ls_123
```

### Options

```
  -h, --help   help for leases
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
* [dtt leases revoke](dtt_leases_revoke.md)	 - Revoke a lease and stop its unowned tunnel

