## dtt expose

Expose a local service publicly through ngrok or cloudflared

### Synopsis

Replace ngrok http 3000 or cloudflared tunnel --url http://localhost:3000. Always warns on stderr that the service will be public. --via is required: settings.toml currently has no default_expose setting. --secret is a keychain secret NAME, never a token. ngrok accepts --domain; cloudflared uses a quick tunnel unless --secret selects a token-based named tunnel, where --domain describes an already-configured public hostname (it does not create routing). --save promotes after readiness. Background tunnels use a detached, revocable person lease, not a pin. They survive CLI exit until dtt stop or lease revocation; --ttl adds an expiry (maximum 8h). Foreground leases default to 30m and Ctrl-C closes the lease.

```
dtt expose <port|url> [flags]
```

### Examples

```
  dtt expose 3000 --via cloudflared
  dtt expose http://localhost:3000 --via ngrok --secret ngrok-work --domain demo.example.com
  dtt expose 3000 --via ngrok --dry-run
```

### Options

```
      --domain string   ngrok domain, or already-routed hostname for a token-based named Cloudflare tunnel
      --dry-run         Print the evaluated command and findings without starting (auto ports need a concrete port for preview)
      --foreground      Hold the CLI open; Ctrl-C closes its lease
  -h, --help            help for expose
      --save string     Promote to a saved tunnel with this name after readiness
      --secret string   Existing keychain secret NAME, never a token value
      --ttl string      Stop after a positive duration, at most 8h (background default: no expiry; foreground: 30m)
      --via string      Public service: ngrok or cloudflared (required)
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

