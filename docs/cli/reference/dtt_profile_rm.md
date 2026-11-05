## dtt profile rm

Delete a profile, keeping its tunnels

### Synopsis

Delete a profile and remove its membership from tunnels. Tunnel definitions and running processes are kept. Removing the active profile clears the active selection.

```
dtt profile rm <p> [flags]
```

### Examples

```
  dtt profile rm work
  dtt profile rm work --json
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

* [dtt profile](dtt_profile.md)	 - Organize tunnels into profiles

