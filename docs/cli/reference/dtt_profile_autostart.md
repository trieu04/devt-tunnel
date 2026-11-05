## dtt profile autostart

Set a profile member's autostart behavior

### Synopsis

Enable or disable autostart for an existing member. Autostart members run when a profile with start_autostart activation behavior is activated.

```
dtt profile autostart <p> <t> on|off [flags]
```

### Examples

```
  dtt profile autostart work db on
  dtt profile autostart work db off --json
```

### Options

```
  -h, --help   help for autostart
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

