## devt-tunnel secret set

Store a credential from a hidden prompt or stdin

### Synopsis

Store a credential of the specified kind. Read a hidden value from the terminal, or use --stdin to read all input with one trailing newline removed. Without a TTY, --stdin is required. Empty values are rejected. Waiting tunnel starts retry after storage. Values are never accepted in flags or positional arguments.

```
devt-tunnel secret set <name> [flags]
```

### Examples

```
  devt-tunnel secret set ngrok-personal --kind ngrok
  op read op://dev/ngrok/token | devt-tunnel secret set ngrok-personal --kind ngrok --stdin
```

### Options

```
  -h, --help          help for set
      --kind string   Credential kind: ssh_passphrase, ngrok, cloudflared or env (required)
      --stdin         Read the value from stdin instead of a hidden terminal prompt
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

