## dtt profile

Manage configuration shared by tunnels

### Synopsis

Manage named configurations, such as the SSH config file, used by the tunnels that belong to them. A tunnel belongs to at most one profile. To start or stop tunnels together, use dtt group. Profile references accept an exact ID, exact name or unique ID prefix.

```
dtt profile [flags]
```

### Examples

```
  dtt profile ls
  dtt profile add Work --ssh-config ~/.ssh/work
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt profile add](dtt_profile_add.md)	 - Create a profile
* [dtt profile ls](dtt_profile_ls.md)	 - List profiles and their members
* [dtt profile rm](dtt_profile_rm.md)	 - Delete a profile, keeping its tunnels
* [dtt profile show](dtt_profile_show.md)	 - Inspect a profile and its members

