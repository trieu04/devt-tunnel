## devt-tunnel ls

List tunnels and their endpoints

### Synopsis

List saved and ephemeral tunnels, their states, endpoints and ownership. Replaces ps aux | grep ssh and lsof -i :PORT. JSON includes pinned, ephemeral and lease ownership fields.

```
devt-tunnel ls [flags]
```

### Examples

```
  devt-tunnel ls
  devt-tunnel ls --running --type ssh
  devt-tunnel ls --profile work --favorites --json
```

### Options

```
      --favorites        Only favorite tunnels
      --group string     Filter by group ID
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

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels

