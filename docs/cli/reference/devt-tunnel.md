## devt-tunnel

Supervise and inspect development tunnels

### Synopsis

Manage development tunnels through the per-user daemon. Use traditional ssh, ngrok and cloudflared commands under one supervisor.

```
devt-tunnel [flags]
```

### Examples

```
  devt-tunnel ls
  devt-tunnel show db
  devt-tunnel daemon status
```

### Options

```
  -h, --help               help for devt-tunnel
      --json               Write versioned JSON results and errors
      --no-color           Disable color (also NO_COLOR)
      --no-daemon-start    Do not start the daemon automatically
      --no-input           Never prompt for input
  -q, --quiet              Suppress informational diagnostics
      --timeout duration   Daemon connection and request timeout (default 10s)
```

### SEE ALSO

* [devt-tunnel add](devt-tunnel_add.md)	 - Define and save a supervised tunnel
* [devt-tunnel addr](devt-tunnel_addr.md)	 - Print a tunnel's address
* [devt-tunnel approvals](devt-tunnel_approvals.md)	 - List pending agent requests for a person
* [devt-tunnel approve](devt-tunnel_approve.md)	 - Approve a pending agent request
* [devt-tunnel audit](devt-tunnel_audit.md)	 - Read or follow the daemon's audit history
* [devt-tunnel check](devt-tunnel_check.md)	 - Run editor checks for a saved tunnel
* [devt-tunnel completion](devt-tunnel_completion.md)	 - Generate a shell completion script
* [devt-tunnel config](devt-tunnel_config.md)	 - Locate, reload or edit tunnel configuration
* [devt-tunnel daemon](devt-tunnel_daemon.md)	 - Control the per-user daemon
* [devt-tunnel deny](devt-tunnel_deny.md)	 - Deny a pending agent request
* [devt-tunnel docs](devt-tunnel_docs.md)	 - Read embedded offline guides
* [devt-tunnel doctor](devt-tunnel_doctor.md)	 - Check tools, keychain and daemon health
* [devt-tunnel dup](devt-tunnel_dup.md)	 - Save a copy of a tunnel
* [devt-tunnel edit](devt-tunnel_edit.md)	 - Edit a tunnel as a TOML fragment
* [devt-tunnel expose](devt-tunnel_expose.md)	 - Expose a local service publicly through ngrok or cloudflared
* [devt-tunnel forward](devt-tunnel_forward.md)	 - Open a background ephemeral SSH forward
* [devt-tunnel group](devt-tunnel_group.md)	 - Operate on sets of tunnels together
* [devt-tunnel hosts](devt-tunnel_hosts.md)	 - List SSH config aliases or resolve one
* [devt-tunnel import](devt-tunnel_import.md)	 - Save a traditional command as a tunnel
* [devt-tunnel leases](devt-tunnel_leases.md)	 - List clients holding tunnels and their expiries
* [devt-tunnel logs](devt-tunnel_logs.md)	 - Read or follow retained tunnel output
* [devt-tunnel ls](devt-tunnel_ls.md)	 - List tunnels and their endpoints
* [devt-tunnel man](devt-tunnel_man.md)	 - Write man pages for every command
* [devt-tunnel mcp](devt-tunnel_mcp.md)	 - Connect agents through the policy-controlled MCP adapter
* [devt-tunnel orphans](devt-tunnel_orphans.md)	 - List tunnel processes left after a crash
* [devt-tunnel profile](devt-tunnel_profile.md)	 - Manage configuration shared by tunnels
* [devt-tunnel prompts](devt-tunnel_prompts.md)	 - List SSH questions waiting for an answer
* [devt-tunnel restart](devt-tunnel_restart.md)	 - Restart tunnels and wait until ready
* [devt-tunnel rm](devt-tunnel_rm.md)	 - Delete saved tunnel definitions
* [devt-tunnel run](devt-tunnel_run.md)	 - Supervise a traditional tunnel command in the foreground
* [devt-tunnel save](devt-tunnel_save.md)	 - Promote an ephemeral tunnel to a saved definition
* [devt-tunnel secret](devt-tunnel_secret.md)	 - Manage named credentials without displaying values
* [devt-tunnel set](devt-tunnel_set.md)	 - Change tunnel fields without an editor
* [devt-tunnel show](devt-tunnel_show.md)	 - Inspect a tunnel definition and runtime state
* [devt-tunnel start](devt-tunnel_start.md)	 - Start tunnels and wait until ready
* [devt-tunnel stop](devt-tunnel_stop.md)	 - Stop tunnels and their process trees
* [devt-tunnel tools](devt-tunnel_tools.md)	 - List tunnel binaries and their versions
* [devt-tunnel version](devt-tunnel_version.md)	 - Show CLI and reachable daemon versions
* [devt-tunnel wait](devt-tunnel_wait.md)	 - Wait for a tunnel to become ready

