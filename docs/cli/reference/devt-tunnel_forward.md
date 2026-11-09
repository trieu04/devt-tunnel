## devt-tunnel forward

Open a background ephemeral SSH forward

### Synopsis

Replace ssh -f -N -L 127.0.0.1:5433:127.0.0.1:5432 server1 without using ssh -f. Maps accept port:port, port:host:port or bind:port:host:port; auto:5432 requests a free loopback port from the daemon. Non-loopback binds warn. Repeat --remote or --socks for remote forwards and SOCKS listeners. SSH config aliases (including user@host destinations) are handled by ssh, using a profile's SSH config with --profile. --save promotes after readiness without restarting. Background tunnels use a detached, revocable person lease, not a pin. They survive CLI exit until devt-tunnel stop or lease revocation; --ttl adds an expiry (maximum 8h). Foreground leases default to 30m and Ctrl-C closes the lease.

```
devt-tunnel forward <host> [map...] [flags]
```

### Examples

```
  devt-tunnel forward server1 auto:5432 --json
  devt-tunnel forward server1 5433:db:5432 --ttl 15m --save staging-db
  devt-tunnel forward server1 --remote 8080:3000 --socks 1080 --foreground
```

### Options

```
      --dry-run              Print the evaluated command and findings without starting (auto ports need a concrete port for preview)
      --foreground           Hold the CLI open; Ctrl-C closes its lease
  -h, --help                 help for forward
      --profile string       Use this profile's SSH config (ID, name or unique ID prefix)
      --remote stringArray   Remote port:port, port:host:port or bind:port:host:port (repeatable)
      --save string          Promote to a saved tunnel with this name after readiness
      --socks stringArray    SOCKS listen port, bind:port or auto (repeatable)
      --ttl string           Stop after a positive duration, at most 8h (background default: no expiry; foreground: 30m)
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

