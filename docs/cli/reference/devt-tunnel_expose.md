## devt-tunnel expose

Expose a local service publicly through ngrok or cloudflared

### Synopsis

Replace ngrok http 3000 or cloudflared tunnel --url http://localhost:3000. Always warns on stderr that the service will be public. --via is required: settings.toml currently has no default_expose setting. --secret is a keychain secret NAME, never a token. ngrok accepts --domain; cloudflared uses a quick tunnel unless --secret selects a token-based named tunnel, where --domain describes an already-configured public hostname (it does not create routing). --save promotes after readiness. Background tunnels are detached, not pinned. They survive CLI exit until devt-tunnel stop. Foreground tunnels stop when the CLI exits or on Ctrl-C.

```
devt-tunnel expose <port|url> [flags]
```

### Examples

```
  devt-tunnel expose 3000 --via cloudflared
  devt-tunnel expose http://localhost:3000 --via ngrok --secret ngrok-work --domain demo.example.com
  devt-tunnel expose 3000 --via ngrok --dry-run
```

### Options

```
      --domain string   ngrok domain, or already-routed hostname for a token-based named Cloudflare tunnel
      --dry-run         Print the evaluated command and findings without starting (auto ports need a concrete port for preview)
      --foreground      Hold the CLI open; Ctrl-C stops the tunnel
  -h, --help            help for expose
      --save string     Promote to a saved tunnel with this name after readiness
      --secret string   Existing keychain secret NAME, never a token value
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

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels

