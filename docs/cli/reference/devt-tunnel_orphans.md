## devt-tunnel orphans

List tunnel processes left after a crash

### Synopsis

List tunnel processes recovered from crash records. The daemon checks OS process identities to protect against PID reuse. Replaces searching ps output for abandoned tunnel processes.

```
devt-tunnel orphans [flags]
```

### Examples

```
  devt-tunnel orphans
  devt-tunnel orphans --json
  devt-tunnel orphans stop 12345
```

### Options

```
  -h, --help   help for orphans
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
* [devt-tunnel orphans stop](devt-tunnel_orphans_stop.md)	 - Stop a recovered orphan process

