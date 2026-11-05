## dtt mcp tools

Review MCP tool descriptions, schemas and annotations

### Synopsis

Print the tool definitions exported by the adapter, including descriptions, input/output schemas and safety annotations, without contacting the daemon. With --json, stdout is the raw MCP tool array, not a versioned CLI envelope. Review these definitions before enabling an agent client; the daemon remains the authority for policy and approval.

```
dtt mcp tools [flags]
```

### Examples

```
  dtt mcp tools
  dtt mcp tools --json
```

### Options

```
  -h, --help   help for tools
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

