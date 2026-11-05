## dtt profile show

Inspect a profile and its members

### Synopsis

Show a profile's active state, SSH config, activation behavior, members and each member's autostart setting.

```
dtt profile show <p> [flags]
```

### Examples

```
  dtt profile show work
  dtt profile show 'Work projects' --json
```

### Options

```
  -h, --help   help for show
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

