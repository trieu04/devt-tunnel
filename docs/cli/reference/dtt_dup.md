## dtt dup

Save a copy of a tunnel

### Synopsis

Copy a tunnel through the daemon editor's duplicate draft operation, evaluate it and save it with a new stable ID. The original and its profile memberships are preserved. Replaces copying a tunnel block in tunnels.toml.

```
dtt dup <t> --name N [flags]
```

### Examples

```
  dtt dup db --name db-staging
```

### Options

```
  -h, --help          help for dup
      --name string   Name of the new tunnel (required)
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

