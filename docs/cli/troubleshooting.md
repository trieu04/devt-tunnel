# Troubleshooting

| Message | Cause | Fix |
|---|---|---|
| `127.0.0.1:5433 is already in use` | Another tunnel or process has it | `devt-tunnel ls`, `lsof -i :5433`, or use `auto:5432` |
| `secret "x" is missing` | Not in the keychain | `devt-tunnel secret set x --kind env` |
| `the OS keychain is unavailable` | Common on Linux without Secret Service | Install one, or use tunnels without secrets. There is no plaintext fallback |
| `prompt waiting: host key` | First connection to this host | Run on a TTY, or `devt-tunnel prompts`, or add the key with `ssh` first |
| `tunnels.toml changed on disk` | Edited by hand | `devt-tunnel config reload`, then repeat the change |
| `daemon version 2 is newer than this CLI` | Mixed install | Upgrade `devt-tunnel` |
| `ssh was not found` | Not on the daemon's PATH | `devt-tunnel tools set ssh /path/to/ssh` |
| `tool "cloudflared" was not found` | cloudflared is not installed | `devt-tunnel tools download cloudflared`, or install it and run `devt-tunnel tools candidates cloudflared` |
| `checksum_mismatch` from `tools download` | The file did not match the hash pinned in devt-tunnel; it was deleted | Retry; if it persists, a proxy or mirror is altering the download. Install cloudflared another way |
| `network` from `tools download` | The transfer failed or the connection dropped | Check the connection or `HTTPS_PROXY`, then retry |

## Exit codes

Scripts can rely on these.

| Code | Name | Meaning |
|---|---|---|
| 0 | | Success |
| 1 | `failed` | The tunnel or operation failed, including a person stopping an MCP-opened tunnel (`tunnel_stopped`), a policy refusal (`policy_denied`), disabled MCP access (`agent_disabled`), or a tool download that failed its checksum (`checksum_mismatch`) or transfer (`network`). See the message and `devt-tunnel logs db` |
| 2 | `usage` | Bad arguments or an invalid tunnel specification (`invalid_spec`), or an unknown or ambiguous tunnel |
| 3 | `needs_secret` | A required secret is missing, or the keychain is unavailable |
| 4 | `needs_user` | A prompt needs a person and no TTY is available (or `--no-input`). Includes `pending_approval`; poll the approval and retry after a person decides. |
| 5 | `conflict` | Port in use (`port_in_use`), or `tunnels.toml` changed on disk |
| 6 | `timeout` | Not ready within the timeout |
| 7 | `daemon` | The daemon cannot be reached or started, or its version is incompatible |
| 8 | `not_found` | A required binary is missing, e.g. `ngrok` |

Keep this table in step with `internal/api` error codes, which MCP also uses.

Use `devt-tunnel doctor` to inspect the daemon, binaries and keychain, `devt-tunnel tools` for tool paths, and `devt-tunnel prompts` for pending SSH questions. `devt-tunnel version` never starts a daemon.
