## dtt start

Start tunnels and wait until ready

### Synopsis

Start saved tunnels, like ssh -N -L or ngrok http, and wait until they are ready. Starting an already active tunnel is a no-op. SSH questions are shown inline on a TTY; otherwise exit 4 and use dtt prompts. --timeout bounds the readiness wait; --no-wait returns after requesting the starts.

```
dtt start <t>... [flags]
```

### Examples

```
  dtt start db
  dtt start db web --timeout 30s
  dtt start db --no-wait
```

### Options

```
  -h, --help      help for start
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels

