## dtt run

Supervise a traditional tunnel command in the foreground

### Synopsis

Import ssh -N -L ..., ngrok http ... or cloudflared tunnel ... using the same importer as dtt import, then start an ephemeral tunnel and print its address when ready. SSH questions appear inline on a TTY. Inline credentials are rejected. --detach returns once ready. Background tunnels use a detached, revocable person lease, not a pin. They survive CLI exit until dtt stop or lease revocation; --ttl adds an expiry (maximum 8h). Foreground leases default to 30m and Ctrl-C closes the lease.

```
dtt run -- <cmd> [args...] [flags]
```

### Examples

```
  dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1
  dtt run --detach --ttl 15m -- ngrok http 3000
  dtt run --dry-run -- cloudflared tunnel --url http://localhost:3000
```

### Options

```
      --detach        Return once ready, retaining a detached lease until --ttl, stop or revoke
      --dry-run       Print the evaluated command and findings without starting (auto ports need a concrete port for preview)
  -h, --help          help for run
      --name string   Display name (default: TYPE tunnel)
      --ttl string    Stop after a positive duration, at most 8h (background default: no expiry; foreground: 30m)
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

