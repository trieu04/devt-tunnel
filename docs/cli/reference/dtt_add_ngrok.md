## dtt add ngrok

Save a public ngrok tunnel

### Synopsis

Save the equivalent of ngrok http http://127.0.0.1:3000, with log output configured for supervision. --secret is an existing keychain secret NAME, never an authtoken value. The tunnel will be public when started.

```
dtt add ngrok <name> --proto http --addr A [flags]
```

### Examples

```
  dtt add ngrok web --proto http --addr http://localhost:3000 --secret ngrok-work
  dtt add ngrok tcp-db --proto tcp --addr localhost:5432 --dry-run
```

### Options

```
      --addr string         Local service address (required)
      --domain string       Public ngrok domain
      --dry-run             Print the exact evaluated command and findings without saving or starting
      --favorite            Mark as a favorite
      --group stringArray   Add to an existing group by ID, name or unique ID prefix (repeatable)
  -h, --help                help for ngrok
      --no-reconnect        Disable automatic reconnection
      --profile string      Put in an existing profile by ID, name or unique ID prefix
      --proto string        ngrok protocol: http, tcp or tls (default "http")
      --secret string       Existing keychain secret NAME for ngrok authentication; never a secret value
      --start               Save and start the tunnel
      --start-on-launch     Start when the app launches
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

* [dtt add](dtt_add.md)	 - Define and save a supervised tunnel

