## devt-tunnel config reload

Reload and validate tunnel configuration

### Synopsis

Reload tunnels.toml from disk and report config errors. Invalid configuration leaves the daemon's last good definitions intact and exits with code 1; concurrent config conflicts exit with code 5.

```
devt-tunnel config reload [flags]
```

### Examples

```
  devt-tunnel config reload
  devt-tunnel config reload --json
```

### Options

```
  -h, --help   help for reload
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

