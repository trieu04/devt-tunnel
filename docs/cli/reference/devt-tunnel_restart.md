## devt-tunnel restart

Restart tunnels and wait until ready

### Synopsis

Stop saved tunnels, then start them again and wait until ready, like kill <pid> followed by ssh -N -L or ngrok http. SSH questions are shown inline on a TTY; otherwise exit 4 and use devt-tunnel prompts. --timeout bounds the readiness wait.

```
devt-tunnel restart <t>... [flags]
```

### Examples

```
  devt-tunnel restart db
  devt-tunnel restart db web --timeout 30s
  devt-tunnel restart db --no-wait
```

### Options

```
  -h, --help      help for restart
      --no-wait   Return after requesting starts, without waiting for readiness
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

