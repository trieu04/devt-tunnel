## dtt mcp serve

Serve MCP JSON-RPC on stdin and stdout

### Synopsis

Run the stdio-only MCP adapter until the client disconnects or the context is canceled. Only JSON-RPC is written to stdout, even with --json; CLI errors and diagnostics go to stderr. The daemon starts on demand unless --no-daemon-start is set. --name overrides the MCP client's label in the app and audit log. Agents cannot approve requests, answer SSH prompts or set secrets.

```
dtt mcp serve [flags]
```

### Examples

```
  dtt mcp serve
  dtt mcp serve --name coding-agent
  dtt mcp serve --no-daemon-start
```

### Options

```
  -h, --help          help for serve
      --name string   Client label shown in the app and audit log (default: MCP client name)
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

* [dtt mcp](dtt_mcp.md)	 - Connect agents through the policy-controlled MCP adapter

