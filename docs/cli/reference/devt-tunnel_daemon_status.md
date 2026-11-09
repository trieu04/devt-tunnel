## devt-tunnel daemon status

Show daemon status

### Synopsis

Show the daemon version, API version, PID, connections and tunnel states without starting it.

```
devt-tunnel daemon status [flags]
```

### Examples

```
  devt-tunnel daemon status
  devt-tunnel daemon status --json --timeout 20s
```

### Options

```
  -h, --help   help for status
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

