## devt-tunnel profile rm

Delete a profile, keeping its tunnels

### Synopsis

Delete a profile. Its tunnels are kept, left running, and go back to the default SSH config.

```
devt-tunnel profile rm <p> [flags]
```

### Examples

```
  devt-tunnel profile rm work
  devt-tunnel profile rm work --json
```

### Options

```
  -h, --help   help for rm
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

