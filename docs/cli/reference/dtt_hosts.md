## dtt hosts

List SSH config aliases or resolve one

### Synopsis

List literal SSH aliases and their source files. --profile selects a profile's SSH config. --resolve shows effective connection settings, equivalent to ssh -G ALIAS (and ssh -F CONFIG -G ALIAS for a profile-specific config).

```
dtt hosts [flags]
```

### Examples

```
  dtt hosts
  dtt hosts --profile work
  dtt hosts --resolve server1 --json
```

### Options

```
  -h, --help             help for hosts
      --profile string   Use this profile ID's SSH config
      --resolve string   Resolve an alias through ssh -G
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

