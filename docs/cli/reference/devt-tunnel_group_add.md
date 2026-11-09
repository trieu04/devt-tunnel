## devt-tunnel group add

Create a group

### Synopsis

Create an empty group. Assign tunnels with devt-tunnel group assign or devt-tunnel add --group.

```
devt-tunnel group add <name> [flags]
```

### Examples

```
  devt-tunnel group add Backend
  devt-tunnel group add Staging --description 'Everything for staging'
```

### Options

```
      --description string   What the group is for
  -h, --help                 help for add
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

