## dtt doctor

Check tools, keychain and daemon health

### Synopsis

Check daemon connectivity, tool paths and versions, OS keychain availability, config errors, SSH config warnings and whether dtt is on the shell PATH. Replaces command -v ssh, ssh -V, ngrok version and cloudflared --version. Missing tools are essential only when a saved tunnel needs them; SSH config and shell PATH issues are warnings.

```
dtt doctor [flags]
```

### Examples

```
  dtt doctor
  dtt doctor --json --no-daemon-start
```

### Options

```
  -h, --help   help for doctor
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

