## dtt stop

Stop tunnels and their process trees

### Synopsis

Stop saved tunnels and their entire process trees, replacing kill <pid>. Select tunnel references, --all, or --profile P, but do not combine these selectors. Stopping an already stopped tunnel succeeds.

```
dtt stop [t]... [flags]
```

### Examples

```
  dtt stop db web
  dtt stop --all
  dtt stop --profile work
```

### Options

```
      --all              Stop all tunnels
  -h, --help             help for stop
      --profile string   Stop members of a profile (ID, name or unique ID prefix)
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

