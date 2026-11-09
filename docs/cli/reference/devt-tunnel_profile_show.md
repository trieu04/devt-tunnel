## devt-tunnel profile show

Inspect a profile and its members

### Synopsis

Show a profile's description, SSH config and members.

```
devt-tunnel profile show <p> [flags]
```

### Examples

```
  devt-tunnel profile show work
  devt-tunnel profile show 'Work projects' --json
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

* [devt-tunnel profile](devt-tunnel_profile.md)	 - Manage configuration shared by tunnels

