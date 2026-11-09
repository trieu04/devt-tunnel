## devt-tunnel daemon run

Run the daemon in the foreground

### Synopsis

Run the daemon in the foreground for service units and debugging. It exits after five idle minutes by default; --idle-exit 0 disables this.

```
devt-tunnel daemon run [flags]
```

### Examples

```
  devt-tunnel daemon run
  devt-tunnel daemon run --idle-exit 0
```

### Options

```
  -h, --help                 help for run
      --idle-exit duration   Exit after idle duration; 0 disables idle exit (default 5m0s)
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

