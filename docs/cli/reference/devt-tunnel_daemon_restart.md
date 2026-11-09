## devt-tunnel daemon restart

Restart the daemon, stopping all tunnels

### Synopsis

Stop the daemon and all tunnels, then start the current daemon version. Also starts a daemon if none is running.

```
devt-tunnel daemon restart [flags]
```

### Examples

```
  devt-tunnel daemon restart
  devt-tunnel daemon restart --json --timeout 20s
```

### Options

```
  -h, --help   help for restart
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

