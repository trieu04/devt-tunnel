## devt-tunnel daemon

Control the per-user daemon

### Synopsis

Run or manage the per-user tunnel daemon. Stopping or restarting it stops all tunnels.

```
devt-tunnel daemon [flags]
```

### Examples

```
  devt-tunnel daemon status
  devt-tunnel daemon restart --timeout 20s
```

### Options

```
  -h, --help   help for daemon
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
* [devt-tunnel daemon restart](devt-tunnel_daemon_restart.md)	 - Restart the daemon, stopping all tunnels
* [devt-tunnel daemon run](devt-tunnel_daemon_run.md)	 - Run the daemon in the foreground
* [devt-tunnel daemon status](devt-tunnel_daemon_status.md)	 - Show daemon status
* [devt-tunnel daemon stop](devt-tunnel_daemon_stop.md)	 - Stop the daemon and all tunnels

