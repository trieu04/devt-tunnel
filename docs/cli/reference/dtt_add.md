## dtt add

Define and save a supervised tunnel

### Synopsis

Define an ssh, ngrok, cloudflared or command tunnel using the same defaults and evaluation as the app editor. --dry-run prints the exact command that would run and never saves or starts it. Secret flags accept existing keychain names only, never credential values.

```
dtt add [flags]
```

### Examples

```
  dtt add ssh db --host server1 -L 15432:5432
  dtt add cloudflared web --url http://localhost:3000 --dry-run
```

### Options

```
  -h, --help   help for add
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
* [dtt add cloudflared](dtt_add_cloudflared.md)	 - Save a public quick or named Cloudflare Tunnel
* [dtt add command](dtt_add_command.md)	 - Save an arbitrary supervised shell command
* [dtt add ngrok](dtt_add_ngrok.md)	 - Save a public ngrok tunnel
* [dtt add ssh](dtt_add_ssh.md)	 - Save an SSH tunnel with local, remote or SOCKS forwards

