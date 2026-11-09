## devt-tunnel mcp config

Print client configuration with this executable's absolute path

### Synopsis

Print a shell-quoted Claude Code registration command and an mcpServers JSON configuration using this executable's absolute, symlink-resolved path. No daemon is needed. With --json, stdout is only the paste-ready configuration object {"mcpServers":{"devt-tunnel":{"command":"/absolute/devt-tunnel","args":["mcp","serve"]}}}, deliberately without the usual apiVersion envelope. Temporary go run/test executable paths produce a stderr warning; install a stable binary before registering it.

```
devt-tunnel mcp config [flags]
```

### Examples

```
  devt-tunnel mcp config
  devt-tunnel mcp config --json > mcp-config.json
```

### Options

```
  -h, --help   help for config
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

* [devt-tunnel mcp](devt-tunnel_mcp.md)	 - Connect agents through the policy-controlled MCP adapter

