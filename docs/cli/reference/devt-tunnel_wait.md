## devt-tunnel wait

Wait for a tunnel to become ready

### Synopsis

Wait for a tunnel that is starting or reconnecting to become running, instead of polling ps or a listening port with nc. Does not start a stopped tunnel. --timeout bounds the readiness wait. SSH questions use the same TTY behavior as devt-tunnel start.

```
devt-tunnel wait <t> [flags]
```

### Examples

```
  devt-tunnel wait db
  devt-tunnel wait db --timeout 30s --json
```

### Options

```
  -h, --help   help for wait
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

