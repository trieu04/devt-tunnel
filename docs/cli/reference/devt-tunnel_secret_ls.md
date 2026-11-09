## devt-tunnel secret ls

List credential metadata, never values

### Synopsis

List credential names, kinds, tunnels using them, last use and presence state. No secret values are returned. An unavailable OS keychain exits with code 3.

```
devt-tunnel secret ls [flags]
```

### Examples

```
  devt-tunnel secret ls
  devt-tunnel secret ls --json
```

### Options

```
  -h, --help   help for ls
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

* [devt-tunnel secret](devt-tunnel_secret.md)	 - Manage named credentials without displaying values

