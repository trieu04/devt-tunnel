## devt-tunnel addr

Print a tunnel's address

### Synopsis

Print the first endpoint without labels, suitable for shell substitution. --all prints every endpoint; --port prints just the first endpoint's port. Replaces extracting a forward's listen address from ssh -L or process arguments.

```
devt-tunnel addr <t> [flags]
```

### Examples

```
  devt-tunnel addr db
  psql "host=127.0.0.1 port=$(devt-tunnel addr db --port) dbname=app"
  devt-tunnel addr db --all
```

### Options

```
      --all    Print every endpoint
  -h, --help   help for addr
      --port   Print only the first endpoint's port
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

