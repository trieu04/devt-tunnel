## dtt profile use

Set or clear the active profile

### Synopsis

Activate a profile, applying its start-autostart and stop-non-members behavior. --none clears the active profile without starting or stopping tunnels.

```
dtt profile use <p> | --none [flags]
```

### Examples

```
  dtt profile use work
  dtt profile use --none
```

### Options

```
  -h, --help   help for use
      --none   Clear the active profile
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

