## devt-tunnel tools

List tunnel binaries and their versions

### Synopsis

Show ssh, ngrok and cloudflared paths, versions, discovery sources and errors using the daemon's environment. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version.

```
devt-tunnel tools [flags]
```

### Examples

```
  devt-tunnel tools
  devt-tunnel tools --json
  devt-tunnel tools set ssh /usr/bin/ssh
```

### Options

```
  -h, --help   help for tools
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

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels
* [devt-tunnel tools set](devt-tunnel_tools_set.md)	 - Set or clear a per-machine tool path

