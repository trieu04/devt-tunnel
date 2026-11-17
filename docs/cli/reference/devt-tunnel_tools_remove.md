## devt-tunnel tools remove

Delete a downloaded tool version

### Synopsis

Delete one version that devt-tunnel downloaded into its tools directory. Refused with conflict while a running tunnel uses it. If that version is the selected override, the override is cleared first so tunnels return to automatic discovery. Nothing outside the tools directory is ever deleted.

```
devt-tunnel tools remove <tool> <version> [flags]
```

### Examples

```
  devt-tunnel tools remove cloudflared 2026.11.0
  devt-tunnel tools remove cloudflared 2026.11.0 --json
```

### Options

```
  -h, --help   help for remove
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

* [devt-tunnel tools](devt-tunnel_tools.md)	 - List tunnel binaries and their versions

