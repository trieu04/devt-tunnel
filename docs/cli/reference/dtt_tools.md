## dtt tools

List tunnel binaries and their versions

### Synopsis

Show ssh, ngrok and cloudflared paths, versions, discovery sources and errors using the daemon's environment. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version.

```
dtt tools [flags]
```

### Examples

```
  dtt tools
  dtt tools --json
  dtt tools set ssh /usr/bin/ssh
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt tools set](dtt_tools_set.md)	 - Set or clear a per-machine tool path

