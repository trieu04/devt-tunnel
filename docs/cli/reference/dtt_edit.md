## dtt edit

Edit a tunnel as a TOML fragment

### Synopsis

Write one tunnel to a private temporary TOML file, open $VISUAL or $EDITOR (falling back to vi, or notepad on Windows), validate through the daemon and save. The ID must not change. Invalid TOML or definitions can be reopened on a TTY. Redirected input requires an explicitly configured VISUAL or EDITOR. Replaces hand-editing a tunnel block in tunnels.toml.

```
dtt edit <t> [flags]
```

### Examples

```
  dtt edit db
  EDITOR='code --wait' dtt edit db
  EDITOR=./update-tunnel dtt edit db --no-input --json
```

### Options

```
  -h, --help   help for edit
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

