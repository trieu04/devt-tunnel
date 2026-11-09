## devt-tunnel logs

Read or follow retained tunnel output

### Synopsis

Print retained tunnel output, like tail -n N or tail -f on a log file. Follow polls until Ctrl-C. With --json each batch is a versioned JSON object on its own line. The current daemon does not timestamp log lines, so --since is unavailable until it does.

```
devt-tunnel logs <t> [flags]
```

### Examples

```
  devt-tunnel logs db -n 50
  devt-tunnel logs db -f
  devt-tunnel logs db --json
```

### Options

```
  -f, --follow           Follow new output until interrupted
  -h, --help             help for logs
  -n, --lines int        Number of retained lines to print (default 100)
      --since duration   Only lines from this duration ago (requires daemon timestamps)
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

