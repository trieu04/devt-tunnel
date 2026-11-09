## devt-tunnel group

Operate on sets of tunnels together

### Synopsis

Manage named sets of tunnels and start or stop them as one task. A tunnel can be in many groups, independently of its profile. Group references accept an exact ID, exact name or unique ID prefix.

```
devt-tunnel group [flags]
```

### Examples

```
  devt-tunnel group add Backend
  devt-tunnel group assign backend db api
  devt-tunnel group start backend
```

### Options

```
  -h, --help   help for group
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
* [devt-tunnel group add](devt-tunnel_group_add.md)	 - Create a group
* [devt-tunnel group assign](devt-tunnel_group_assign.md)	 - Add tunnels to a group
* [devt-tunnel group ls](devt-tunnel_group_ls.md)	 - List groups and their members
* [devt-tunnel group rm](devt-tunnel_group_rm.md)	 - Delete a group, keeping its tunnels
* [devt-tunnel group show](devt-tunnel_group_show.md)	 - Inspect a group and its members
* [devt-tunnel group start](devt-tunnel_group_start.md)	 - Start all members of a group
* [devt-tunnel group stop](devt-tunnel_group_stop.md)	 - Stop all members of a group
* [devt-tunnel group unassign](devt-tunnel_group_unassign.md)	 - Remove tunnels from a group

