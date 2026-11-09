## devt-tunnel version

Show CLI and reachable daemon versions

### Synopsis

Show the CLI version and API version, plus the daemon version if reachable. This command never starts a daemon.

```
devt-tunnel version [flags]
```

### Examples

```
  devt-tunnel version
  devt-tunnel version --json
```

### Options

```
  -h, --help   help for version
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

