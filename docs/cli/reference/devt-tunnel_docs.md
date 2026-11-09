## devt-tunnel docs

Read embedded offline guides

### Synopsis

List available documentation topics, or print a guide as Markdown. Guides are embedded in the binary and never need the network or daemon. Use devt-tunnel help for command flags and devt-tunnel man to generate a complete manual.

```
devt-tunnel docs [topic] [flags]
```

### Examples

```
  devt-tunnel docs
  devt-tunnel docs quickstart
  devt-tunnel docs troubleshooting
```

### Options

```
  -h, --help   help for docs
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

