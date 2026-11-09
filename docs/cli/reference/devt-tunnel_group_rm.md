## devt-tunnel group rm

Delete a group, keeping its tunnels

### Synopsis

Delete a group and remove its membership from tunnels. Tunnel definitions and running processes are kept.

```
devt-tunnel group rm <g> [flags]
```

### Examples

```
  devt-tunnel group rm backend
  devt-tunnel group rm backend --json
```

### Options

```
  -h, --help   help for rm
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

