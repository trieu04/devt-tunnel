## dtt daemon

Control the per-user daemon

### Synopsis

Run or manage the per-user tunnel daemon. Stopping or restarting it stops all tunnels.

```
dtt daemon [flags]
```

### Examples

```
  dtt daemon status
  dtt daemon restart --timeout 20s
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt daemon restart](dtt_daemon_restart.md)	 - Restart the daemon, stopping all tunnels
* [dtt daemon run](dtt_daemon_run.md)	 - Run the daemon in the foreground
* [dtt daemon status](dtt_daemon_status.md)	 - Show daemon status
* [dtt daemon stop](dtt_daemon_stop.md)	 - Stop the daemon and all tunnels

