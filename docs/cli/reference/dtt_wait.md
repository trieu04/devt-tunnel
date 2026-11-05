## dtt wait

Wait for a tunnel to become ready

### Synopsis

Wait for a tunnel that is starting or reconnecting to become running, instead of polling ps or a listening port with nc. Does not start a stopped tunnel. --timeout bounds the readiness wait. SSH questions use the same TTY behavior as dtt start.

```
dtt wait <t> [flags]
```

### Examples

```
  dtt wait db
  dtt wait db --timeout 30s --json
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels

