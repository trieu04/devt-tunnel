## devt-tunnel daemon stop

Stop the daemon and all tunnels

### Synopsis

Stop the daemon and all tunnels, including their child processes. This replaces manually killing the daemon PID.

```
devt-tunnel daemon stop [flags]
```

### Examples

```
  devt-tunnel daemon stop
  devt-tunnel daemon stop --json --timeout 20s
```

### Options

```
  -h, --help   help for stop
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

* [devt-tunnel daemon](devt-tunnel_daemon.md)	 - Control the per-user daemon

