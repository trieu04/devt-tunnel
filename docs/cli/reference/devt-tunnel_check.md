## devt-tunnel check

Run editor checks for a saved tunnel

### Synopsis

Run the same binary, secret, SSH option and port checks as the editor. Errors fail the command; --strict also fails on warnings. Replaces manually checking binaries with command -v and ports with lsof -i :PORT.

```
devt-tunnel check <t> [flags]
```

### Examples

```
  devt-tunnel check db
  devt-tunnel check db --strict --json
```

### Options

```
  -h, --help     help for check
      --strict   Fail on warnings as well as errors
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

