## devt-tunnel tools candidates

List every installed copy of a tool

### Synopsis

List each ssh, ngrok or cloudflared executable the daemon can find on its search path or in devt-tunnel's own tools directory, with version, origin, whether Auto would pick it and whether it is the selected override. Use tools set to select one.

```
devt-tunnel tools candidates <tool> [flags]
```

### Examples

```
  devt-tunnel tools candidates cloudflared
  devt-tunnel tools candidates ngrok --json
```

### Options

```
  -h, --help   help for candidates
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

