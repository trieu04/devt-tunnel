## devt-tunnel set

Change tunnel fields without an editor

### Synopsis

Change explicit tunnel fields and evaluate before saving. Supported keys are favorite, name, reconnect and start_on_launch. Renaming never changes the tunnel ID. Use edit for backend-specific settings; replaces hand-editing tunnels.toml for simple changes.

```
devt-tunnel set <t> key=value... [flags]
```

### Examples

```
  devt-tunnel set db favorite=true reconnect=false
  devt-tunnel set db 'name=Production database' start_on_launch=true
```

### Options

```
  -h, --help   help for set
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

