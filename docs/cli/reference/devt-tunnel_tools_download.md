## devt-tunnel tools download

Download a pinned cloudflared release

### Synopsis

Download the cloudflared release pinned in this devt-tunnel build into devt-tunnel's own tools directory, check its SHA-256 and size against the values compiled into devt-tunnel, and select it as the tool override unless --no-use is given. The daemon only fetches URLs compiled into it and never downloads on its own. A progress line is shown on stderr when it is a terminal; Ctrl-C cancels and removes the partial file. Exits with not_found when this platform has no pinned release, checksum_mismatch when the file does not match, and network when the transfer fails.

```
devt-tunnel tools download <tool> [flags]
```

### Examples

```
  devt-tunnel tools download cloudflared
  devt-tunnel tools download cloudflared --no-use --json
```

### Options

```
  -h, --help     help for download
      --no-use   Keep the current tool selection
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

