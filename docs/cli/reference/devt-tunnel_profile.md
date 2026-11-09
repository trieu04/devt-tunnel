## devt-tunnel profile

Manage configuration shared by tunnels

### Synopsis

Manage named configurations, such as the SSH config file, used by the tunnels that belong to them. A tunnel belongs to at most one profile. To start or stop tunnels together, use devt-tunnel group. Profile references accept an exact ID, exact name or unique ID prefix.

```
devt-tunnel profile [flags]
```

### Examples

```
  devt-tunnel profile ls
  devt-tunnel profile add Work --ssh-config ~/.ssh/work
```

### Options

```
  -h, --help   help for profile
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
* [devt-tunnel profile add](devt-tunnel_profile_add.md)	 - Create a profile
* [devt-tunnel profile ls](devt-tunnel_profile_ls.md)	 - List profiles and their members
* [devt-tunnel profile rm](devt-tunnel_profile_rm.md)	 - Delete a profile, keeping its tunnels
* [devt-tunnel profile show](devt-tunnel_profile_show.md)	 - Inspect a profile and its members

