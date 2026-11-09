## devt-tunnel stop

Stop tunnels and their process trees

### Synopsis

Stop saved tunnels and their entire process trees, replacing kill <pid>. Select tunnel references, --all, or --group G, but do not combine these selectors. Stopping an already stopped tunnel succeeds.

```
devt-tunnel stop [t]... [flags]
```

### Examples

```
  devt-tunnel stop db web
  devt-tunnel stop --all
  devt-tunnel stop --group backend
```

### Options

```
      --all            Stop all tunnels
      --group string   Stop members of a group (ID, name or unique ID prefix)
  -h, --help           help for stop
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

