## devt-tunnel group start

Start all members of a group

### Synopsis

Start every tunnel belonging to a group.

```
devt-tunnel group start <g> [flags]
```

### Examples

```
  devt-tunnel group start backend
  devt-tunnel group start backend --json
```

### Options

```
  -h, --help   help for start
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

* [devt-tunnel group](devt-tunnel_group.md)	 - Operate on sets of tunnels together

