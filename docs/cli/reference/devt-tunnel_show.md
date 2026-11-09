## devt-tunnel show

Inspect a tunnel definition and runtime state

### Synopsis

Show a tunnel's definition, state, endpoints, exact preview command, missing secrets and editor findings. Replaces ps aux | grep ssh and lsof -i :PORT. Refer to a tunnel by ID, name or unique ID prefix.

```
devt-tunnel show <t> [flags]
```

### Examples

```
  devt-tunnel show db
  devt-tunnel show 'Production DB' --json
```

### Options

```
  -h, --help   help for show
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

