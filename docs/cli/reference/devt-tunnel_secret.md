## devt-tunnel secret

Manage named credentials without displaying values

### Synopsis

Store and manage credentials in the daemon's OS keychain. Values are accepted only through a hidden terminal prompt or explicit --stdin, never through arguments, flags or environment variables. There is no reveal command; use the desktop app for authenticated reveal.

```
devt-tunnel secret [flags]
```

### Examples

```
  devt-tunnel secret ls
  devt-tunnel secret set ngrok-personal --kind ngrok
```

### Options

```
  -h, --help   help for secret
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
* [devt-tunnel secret ls](devt-tunnel_secret_ls.md)	 - List credential metadata, never values
* [devt-tunnel secret rename](devt-tunnel_secret_rename.md)	 - Rename a credential and update its users
* [devt-tunnel secret rm](devt-tunnel_secret_rm.md)	 - Delete a credential from the OS keychain
* [devt-tunnel secret set](devt-tunnel_secret_set.md)	 - Store a credential from a hidden prompt or stdin

