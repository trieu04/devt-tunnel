## devt-tunnel group unassign

Remove tunnels from a group

### Synopsis

Remove tunnels from a group. Tunnels keep their other groups and their profile, and running tunnels are left running.

```
devt-tunnel group unassign <g> <t>... [flags]
```

### Examples

```
  devt-tunnel group unassign backend db api
```

### Options

```
  -h, --help   help for unassign
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

