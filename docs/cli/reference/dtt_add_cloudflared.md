## dtt add cloudflared

Save a public quick or named Cloudflare Tunnel

### Synopsis

Save the equivalent of cloudflared tunnel --url http://localhost:3000 or cloudflared tunnel run NAME. Use --url for a quick tunnel, or --tunnel-name with optional --hostname and --secret for a named tunnel. --secret is an existing keychain secret NAME, never a token value. The tunnel will be public when started.

```
dtt add cloudflared <name> --url U [flags]
```

### Examples

```
  dtt add cloudflared web --url http://localhost:3000
  dtt add cloudflared work --tunnel-name work --hostname work.example.com --secret cloudflare-work --dry-run
```

### Options

```
      --dry-run              Print the exact evaluated command and findings without saving or starting
      --favorite             Mark as a favorite
  -h, --help                 help for cloudflared
      --hostname string      Public hostname metadata for a named tunnel (routing is managed by Cloudflare)
      --no-reconnect         Disable automatic reconnection
      --profile string       Add to an existing profile by ID, name or unique ID prefix
      --secret string        Existing keychain secret NAME for the named tunnel; never a secret value
      --start                Save and start the tunnel
      --start-on-launch      Start when the app launches
      --tunnel-name string   Existing named Cloudflare tunnel (alternative to --url)
      --url string           Local service URL for a quick tunnel (alternative to --tunnel-name)
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

