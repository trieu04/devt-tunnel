## dtt secret

Manage named credentials without displaying values

### Synopsis

Store and manage credentials in the daemon's OS keychain. Values are accepted only through a hidden terminal prompt or explicit --stdin, never through arguments, flags or environment variables. There is no reveal command; use the desktop app for authenticated reveal.

```
dtt secret [flags]
```

### Examples

```
  dtt secret ls
  dtt secret set ngrok-personal --kind ngrok
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

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt secret ls](dtt_secret_ls.md)	 - List credential metadata, never values
* [dtt secret rename](dtt_secret_rename.md)	 - Rename a credential and update its users
* [dtt secret rm](dtt_secret_rm.md)	 - Delete a credential from the OS keychain
* [dtt secret set](dtt_secret_set.md)	 - Store a credential from a hidden prompt or stdin

