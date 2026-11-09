## dtt group

Operate on sets of tunnels together

### Synopsis

Manage named sets of tunnels and start or stop them as one task. A tunnel can be in many groups, independently of its profile. Group references accept an exact ID, exact name or unique ID prefix.

```
dtt group [flags]
```

### Examples

```
  dtt group add Backend
  dtt group assign backend db api
  dtt group start backend
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt group add](dtt_group_add.md)	 - Create a group
* [dtt group assign](dtt_group_assign.md)	 - Add tunnels to a group
* [dtt group ls](dtt_group_ls.md)	 - List groups and their members
* [dtt group rm](dtt_group_rm.md)	 - Delete a group, keeping its tunnels
* [dtt group show](dtt_group_show.md)	 - Inspect a group and its members
* [dtt group start](dtt_group_start.md)	 - Start all members of a group
* [dtt group stop](dtt_group_stop.md)	 - Stop all members of a group
* [dtt group unassign](dtt_group_unassign.md)	 - Remove tunnels from a group

