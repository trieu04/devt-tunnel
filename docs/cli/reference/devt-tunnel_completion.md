## devt-tunnel completion

Generate a shell completion script

### Synopsis

Write a completion script to stdout. Source it in your shell or install it in the shell's completion directory. Tunnel and profile suggestions query only an already-running daemon, silently giving up after 250ms; completion never starts a daemon.

```
devt-tunnel completion bash|zsh|fish|powershell [flags]
```

### Examples

```
  source <(devt-tunnel completion bash)
  devt-tunnel completion zsh > "${fpath[1]}/_devt-tunnel"
  devt-tunnel completion fish > ~/.config/fish/completions/devt-tunnel.fish
  devt-tunnel completion powershell | Out-String | Invoke-Expression
```

### Options

```
  -h, --help   help for completion
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

