## dtt profile ls

List profiles and their members

### Synopsis

List profiles with their SSH config and member IDs. Deleting a profile never deletes its tunnels.

```
dtt profile ls [flags]
```

### Examples

```
  dtt profile ls
  dtt profile ls --json
```

### Options

```
  -h, --help   help for ls
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

* [dtt profile](dtt_profile.md)	 - Manage configuration shared by tunnels

