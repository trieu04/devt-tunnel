## devt-tunnel man

Write man pages for every command

### Synopsis

Generate section 1 man pages in --dir (the current directory by default), creating the directory if necessary. Existing pages with the same names are overwritten. View the root page with man ./devt-tunnel.1, or install the pages into a man1 directory. No daemon or network is needed.

```
devt-tunnel man [flags]
```

### Examples

```
  devt-tunnel man --dir ./man
  man ./man/devt-tunnel.1
```

### Options

```
      --dir string   Directory for generated section 1 man pages (default ".")
  -h, --help         help for man
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

