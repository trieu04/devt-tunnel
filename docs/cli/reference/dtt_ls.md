## dtt ls

List tunnels and their endpoints

### Synopsis

List saved tunnels, their states and endpoints. Replaces ps aux | grep ssh and lsof -i :PORT. Ownership fields are omitted until phase 19 adds ownership and leases.

```
dtt ls [flags]
```

### Examples

```
  dtt ls
  dtt ls --running --type ssh
  dtt ls --profile work --favorites --json
```

### Options

```
      --favorites        Only favorite tunnels
  -h, --help             help for ls
      --profile string   Filter by profile ID
      --running          Only running tunnels
      --type string      Filter by ssh, ngrok, cloudflared or command
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

