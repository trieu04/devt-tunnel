## dtt group ls

List groups and their members

### Synopsis

List groups with their member IDs. Deleting a group never deletes its tunnels.

```
dtt group ls [flags]
```

### Examples

```
  dtt group ls
  dtt group ls --json
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

* [dtt group](dtt_group.md)	 - Operate on sets of tunnels together

