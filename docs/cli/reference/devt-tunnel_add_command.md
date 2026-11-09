## devt-tunnel add command

Save an arbitrary supervised shell command

### Synopsis

Save a shell command as a tunnel, equivalent to running sh -c LINE (or the configured platform shell). Optionally set its working directory and readiness log regular expression. --secret-env maps ENV=NAME to an existing keychain reference; NAME is never a secret value and values never enter the preview command.

```
devt-tunnel add command <name> --line L [flags]
```

### Examples

```
  devt-tunnel add command proxy --line 'my-proxy --port 8080' --ready-log 'Listening'
  devt-tunnel add command api --line './start-api' --dir ~/work/api --secret-env API_KEY=api-work --dry-run
```

### Options

```
      --dir string               Command working directory
      --dry-run                  Print the exact evaluated command and findings without saving or starting
      --favorite                 Mark as a favorite
      --group stringArray        Add to an existing group by ID, name or unique ID prefix (repeatable)
  -h, --help                     help for command
      --line string              Shell command line (required)
      --no-reconnect             Disable automatic reconnection
      --profile string           Put in an existing profile by ID, name or unique ID prefix
      --ready-log string         Regular expression that marks readiness when matched in the log
      --secret-env stringArray   ENV=NAME mapping to an existing keychain secret name; never a secret value (repeatable)
      --start                    Save and start the tunnel
      --start-on-launch          Start when the app launches
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

* [devt-tunnel add](devt-tunnel_add.md)	 - Define and save a supervised tunnel

