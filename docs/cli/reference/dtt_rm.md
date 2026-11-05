## dtt rm

Delete saved tunnel definitions

### Synopsis

Delete saved tunnel definitions and their profile references. Ask y/N on a TTY; --yes is required with redirected input or --no-input. Resolve every reference before deleting anything. Replaces removing tunnel blocks from tunnels.toml.

```
dtt rm <t>... [flags]
```

### Examples

```
  dtt rm db
  dtt rm db staging --yes --json
```

### Options

```
  -h, --help   help for rm
  -y, --yes    Confirm deletion without prompting (required without a TTY)
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

