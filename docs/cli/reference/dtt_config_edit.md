## dtt config edit

Edit tunnel configuration and reload it

### Synopsis

Open tunnels.toml in $VISUAL, falling back to $EDITOR or vi on a terminal. Editor arguments and quoted paths are supported without shell expansion. After the editor exits successfully, reload and report validation errors without replacing the last good definitions. --no-input disables editor invocation.

```
dtt config edit [flags]
```

### Examples

```
  dtt config edit
  EDITOR='code --wait' dtt config edit
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

* [dtt config](dtt_config.md)	 - Locate, reload or edit tunnel configuration

