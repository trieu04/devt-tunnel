## dtt mcp

Connect agents through the policy-controlled MCP adapter

### Synopsis

Serve MCP over stdio, print paste-ready client configuration, or review tool descriptions and schemas offline. The adapter uses the daemon's agent policy, approvals and leases; it does not grant agents person-client privileges.

```
dtt mcp [flags]
```

### Examples

```
  dtt mcp config
  dtt mcp tools --json
  dtt mcp serve --name coding-agent
```

### Options

```
  -h, --help   help for mcp
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
* [dtt mcp config](dtt_mcp_config.md)	 - Print client configuration with this executable's absolute path
* [dtt mcp serve](dtt_mcp_serve.md)	 - Serve MCP JSON-RPC on stdin and stdout
* [dtt mcp tools](dtt_mcp_tools.md)	 - Review MCP tool descriptions, schemas and annotations

