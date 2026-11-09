## dtt save

Promote an ephemeral tunnel to a saved definition

### Synopsis

Turn a running ephemeral tmp- tunnel into a saved tunnel, without restarting its process or losing its leases. Replaces copying a one-off ssh/ngrok/cloudflared command into configuration. Optional --profile puts it in an existing profile after promotion; saving does not pin it or remove its lease expiry.

```
dtt save <tmp-id> --name N [flags]
```

### Examples

```
  dtt save tmp-123 --name staging-db
  dtt save tmp-123 --name staging-db --profile work --json
```

### Options

```
  -h, --help             help for save
      --name string      Name of the saved tunnel (required)
      --profile string   Put the promoted tunnel in an existing profile
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

