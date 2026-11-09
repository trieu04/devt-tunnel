## devt-tunnel orphans stop

Stop a recovered orphan process

### Synopsis

Stop a process listed by devt-tunnel orphans. The daemon verifies its recorded OS identity before stopping it, unlike an unconditional kill PID.

```
devt-tunnel orphans stop <pid> [flags]
```

### Examples

```
  devt-tunnel orphans stop 12345
  devt-tunnel orphans stop 12345 --json
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

* [devt-tunnel orphans](devt-tunnel_orphans.md)	 - List tunnel processes left after a crash

