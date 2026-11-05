## dtt profile add

Create a profile

### Synopsis

Create an empty profile with optional SSH config and activation behavior. Assign tunnels with dtt add --profile or dtt set. By default activating the profile only changes the filter.

```
dtt profile add <name> [flags]
```

### Examples

```
  dtt profile add Work --ssh-config ~/.ssh/work
  dtt profile add Work --on-activate start_autostart --stop-non-members
```

### Options

```
  -h, --help                 help for add
      --on-activate string   Activation behavior: filter or start_autostart (default "filter")
      --ssh-config string    SSH configuration file used by members
      --stop-non-members     Stop running non-members on activation
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

* [dtt profile](dtt_profile.md)	 - Organize tunnels into profiles

