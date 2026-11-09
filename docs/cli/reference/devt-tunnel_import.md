## devt-tunnel import

Save a traditional command as a tunnel

### Synopsis

Import a traditional ssh -N -L ..., ngrok http ..., cloudflared tunnel ... or shell command, then evaluate and save it. Arguments after -- retain their quoting; a single argument can contain an entire pasted command. Importer warnings are shown. Inline credentials are rejected: store them with devt-tunnel secret set and use add --secret NAME instead.

```
devt-tunnel import [--name N] -- <cmd> [args...] [flags]
```

### Examples

```
  devt-tunnel import --name db -- ssh -N -L 15432:127.0.0.1:5432 server1
  devt-tunnel import --dry-run -- 'ngrok http 3000'
```

### Options

```
      --dry-run             Print the exact evaluated command and findings without saving or starting
      --favorite            Mark as a favorite
      --group stringArray   Add to an existing group by ID, name or unique ID prefix (repeatable)
  -h, --help                help for import
      --name string         Name for the imported tunnel (default: TYPE tunnel)
      --no-reconnect        Disable automatic reconnection
      --profile string      Put in an existing profile by ID, name or unique ID prefix
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

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels

