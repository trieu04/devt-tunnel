## dtt profile stop

Stop all members of a profile

### Synopsis

Stop every tunnel belonging to a profile, without changing the active profile or its autostart settings.

```
dtt profile stop <p> [flags]
```

### Examples

```
  dtt profile stop work
  dtt profile stop work --json
```

### Options

```
  -h, --help   help for stop
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

