## devt-tunnel config path

Print the tunnel configuration path

### Synopsis

Ensure tunnels.toml exists, then print its path without labels for shell substitution. Replaces finding the configuration file by hand.

```
devt-tunnel config path [flags]
```

### Examples

```
  devt-tunnel config path
  cat "$(devt-tunnel config path)"
  devt-tunnel config path --json
```

### Options

```
  -h, --help   help for path
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

* [devt-tunnel config](devt-tunnel_config.md)	 - Locate, reload or edit tunnel configuration

