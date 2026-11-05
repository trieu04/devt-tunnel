## dtt audit

Read or follow the daemon's audit history

### Synopsis

Read retained change history, like tail on an audit log, filtered by exact actor name, exact tunnel ID (including deleted IDs) and an inclusive --since duration ago or RFC3339 timestamp. Filters combine with AND. -f polls audit.read with its persistent cursor until Ctrl-C; --json emits one versioned object per batch in follow mode. Warns if retention overtakes the cursor.

```
dtt audit [flags]
```

### Examples

```
  dtt audit --actor claude-code --since 1h
  dtt audit --tunnel staging-db --json
  dtt audit -f --json
```

### Options

```
      --actor string    Exact actor name
  -f, --follow          Follow new audit events until interrupted
  -h, --help            help for audit
      --since string    History since a duration ago (e.g. 1h) or RFC3339 timestamp
      --tunnel string   Exact tunnel ID (also accepts deleted IDs)
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

