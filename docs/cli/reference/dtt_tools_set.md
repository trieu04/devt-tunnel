## dtt tools set

Set or clear a per-machine tool path

### Synopsis

Set an absolute executable path for ssh, ngrok or cloudflared. Pass an empty path or --clear to remove the override and restore automatic discovery. Returns the refreshed tool list, including version or discovery errors.

```
dtt tools set <tool> <path> | set <tool> --clear [flags]
```

### Examples

```
  dtt tools set ngrok /opt/homebrew/bin/ngrok
  dtt tools set ssh --clear
  dtt tools set ssh '' --json
```

### Options

```
      --clear   Reset to automatic tool discovery
  -h, --help    help for set
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

* [dtt tools](dtt_tools.md)	 - List tunnel binaries and their versions

