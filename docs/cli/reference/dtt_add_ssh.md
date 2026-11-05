## dtt add ssh

Save an SSH tunnel with local, remote or SOCKS forwards

### Synopsis

Save the equivalent of ssh -N -L 127.0.0.1:15432:127.0.0.1:5432 server1. Use --host for an SSH config alias, or --hostname with --user, --port and --identity for a manual destination. Repeat -L, -R, -D, --flag and --option. Maps accept port:port, port:host:port or bind:port:host:port; non-loopback binds warn. Saved definitions require concrete listen ports; use dtt forward for auto allocation.

```
dtt add ssh <name> --host H -L M [flags]
```

### Examples

```
  dtt add ssh db --host server1 -L 15432:5432
  dtt add ssh proxy --hostname example.com --user dev --port 22 --identity ~/.ssh/id_ed25519 -D 1080
  dtt add ssh web --host server1 -L 8080:app:80 --flag C --option ServerAliveInterval=15 --dry-run
```

### Options

```
      --dry-run              Print the exact evaluated command and findings without saving or starting
      --favorite             Mark as a favorite
      --flag stringArray     Additional SSH boolean flag, e.g. C or -v (repeatable)
  -h, --help                 help for ssh
      --host string          SSH config host alias (alternative to --hostname)
      --hostname string      Manual SSH hostname (alternative to --host)
      --identity string      Identity file for --hostname
  -L, --local stringArray    Local forward port:port, port:host:port or bind:port:host:port (repeatable)
      --no-reconnect         Disable automatic reconnection
      --option stringArray   SSH option KEY=VALUE (repeatable)
      --port int             SSH port for --hostname (default: SSH's port)
      --profile string       Add to an existing profile by ID, name or unique ID prefix
  -R, --remote stringArray   Remote forward port:port, port:host:port or bind:port:host:port (repeatable)
  -D, --socks stringArray    SOCKS listen port or bind:port (repeatable)
      --start                Save and start the tunnel
      --start-on-launch      Start when the app launches
      --user string          SSH user for --hostname
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

