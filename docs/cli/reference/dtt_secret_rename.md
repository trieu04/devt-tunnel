## dtt secret rename

Rename a credential and update its users

### Synopsis

Rename a credential in the OS keychain and update all saved tunnel references. Values are never displayed or returned.

```
dtt secret rename <from> <to> [flags]
```

### Examples

```
  dtt secret rename ngrok-personal ngrok-work
  dtt secret rename old-token new-token --json
```

### Options

```
  -h, --help   help for rename
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

