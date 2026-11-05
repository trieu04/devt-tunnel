## dtt secret rm

Delete a credential from the OS keychain

### Synopsis

Delete a credential, keeping tunnel references so affected tunnels report a missing token. Ask for confirmation on a terminal; --yes is required without a TTY or with --no-input.

```
dtt secret rm <name> [flags]
```

### Examples

```
  dtt secret rm ngrok-personal
  dtt secret rm ngrok-personal --yes --json
```

### Options

```
  -h, --help   help for rm
      --yes    Confirm deletion without prompting
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

* [dtt secret](dtt_secret.md)	 - Manage named credentials without displaying values

