## dtt config

Locate, reload or edit tunnel configuration

### Synopsis

Work with the daemon's tunnels.toml. Reload validates disk changes before replacing the last good tunnel definitions.

```
dtt config [flags]
```

### Examples

```
  dtt config path
  dtt config edit
  dtt config reload
```

### Options

```
  -h, --help   help for config
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
* [dtt config edit](dtt_config_edit.md)	 - Edit tunnel configuration and reload it
* [dtt config path](dtt_config_path.md)	 - Print the tunnel configuration path
* [dtt config reload](dtt_config_reload.md)	 - Reload and validate tunnel configuration

