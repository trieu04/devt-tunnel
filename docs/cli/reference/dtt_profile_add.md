## dtt profile add

Create a profile

### Synopsis

Create an empty profile with an optional SSH config. Put tunnels in it with dtt add --profile or dtt save --profile.

```
dtt profile add <name> [flags]
```

### Examples

```
  dtt profile add Work --ssh-config ~/.ssh/work
```

### Options

```
      --description string   What the profile is for
  -h, --help                 help for add
      --ssh-config string    SSH configuration file used by members
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

* [dtt profile](dtt_profile.md)	 - Manage configuration shared by tunnels

