## dtt docs

Read embedded offline guides

### Synopsis

List available documentation topics, or print a guide as Markdown. Guides are embedded in the binary and never need the network or daemon. Use dtt help for command flags and dtt man to generate a complete manual.

```
dtt docs [topic] [flags]
```

### Examples

```
  dtt docs
  dtt docs quickstart
  dtt docs troubleshooting
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels

