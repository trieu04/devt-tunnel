## dtt

Supervise and inspect development tunnels

### Synopsis

Manage development tunnels through the per-user daemon. Use traditional ssh, ngrok and cloudflared commands under one supervisor.

```
dtt [flags]
```

### Examples

```
  dtt ls
  dtt show db
  dtt daemon status
```

### Options

```
  -h, --help               help for dtt
      --json               Write versioned JSON results and errors
      --no-color           Disable color (also NO_COLOR)
      --no-daemon-start    Do not start the daemon automatically
      --no-input           Never prompt for input
  -q, --quiet              Suppress informational diagnostics
      --timeout duration   Daemon connection and request timeout (default 10s)
```

### SEE ALSO

* [dtt add](dtt_add.md)	 - Define and save a supervised tunnel
* [dtt addr](dtt_addr.md)	 - Print a tunnel's address
* [dtt approvals](dtt_approvals.md)	 - List pending agent requests for a person
* [dtt approve](dtt_approve.md)	 - Approve a pending agent request
* [dtt audit](dtt_audit.md)	 - Read or follow the daemon's audit history
* [dtt check](dtt_check.md)	 - Run editor checks for a saved tunnel
* [dtt completion](dtt_completion.md)	 - Generate a shell completion script
* [dtt config](dtt_config.md)	 - Locate, reload or edit tunnel configuration
* [dtt daemon](dtt_daemon.md)	 - Control the per-user daemon
* [dtt deny](dtt_deny.md)	 - Deny a pending agent request
* [dtt docs](dtt_docs.md)	 - Read embedded offline guides
* [dtt doctor](dtt_doctor.md)	 - Check tools, keychain and daemon health
* [dtt dup](dtt_dup.md)	 - Save a copy of a tunnel
* [dtt edit](dtt_edit.md)	 - Edit a tunnel as a TOML fragment
* [dtt expose](dtt_expose.md)	 - Expose a local service publicly through ngrok or cloudflared
* [dtt forward](dtt_forward.md)	 - Open a background ephemeral SSH forward
* [dtt group](dtt_group.md)	 - Operate on sets of tunnels together
* [dtt hosts](dtt_hosts.md)	 - List SSH config aliases or resolve one
* [dtt import](dtt_import.md)	 - Save a traditional command as a tunnel
* [dtt leases](dtt_leases.md)	 - List clients holding tunnels and their expiries
* [dtt logs](dtt_logs.md)	 - Read or follow retained tunnel output
* [dtt ls](dtt_ls.md)	 - List tunnels and their endpoints
* [dtt man](dtt_man.md)	 - Write man pages for every command
* [dtt mcp](dtt_mcp.md)	 - Connect agents through the policy-controlled MCP adapter
* [dtt orphans](dtt_orphans.md)	 - List tunnel processes left after a crash
* [dtt profile](dtt_profile.md)	 - Manage configuration shared by tunnels
* [dtt prompts](dtt_prompts.md)	 - List SSH questions waiting for an answer
* [dtt restart](dtt_restart.md)	 - Restart tunnels and wait until ready
* [dtt rm](dtt_rm.md)	 - Delete saved tunnel definitions
* [dtt run](dtt_run.md)	 - Supervise a traditional tunnel command in the foreground
* [dtt save](dtt_save.md)	 - Promote an ephemeral tunnel to a saved definition
* [dtt secret](dtt_secret.md)	 - Manage named credentials without displaying values
* [dtt set](dtt_set.md)	 - Change tunnel fields without an editor
* [dtt show](dtt_show.md)	 - Inspect a tunnel definition and runtime state
* [dtt start](dtt_start.md)	 - Start tunnels and wait until ready
* [dtt stop](dtt_stop.md)	 - Stop tunnels and their process trees
* [dtt tools](dtt_tools.md)	 - List tunnel binaries and their versions
* [dtt version](dtt_version.md)	 - Show CLI and reachable daemon versions
* [dtt wait](dtt_wait.md)	 - Wait for a tunnel to become ready

