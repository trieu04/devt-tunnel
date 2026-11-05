# Troubleshooting

| Message | Cause | Fix |
|---|---|---|
| `127.0.0.1:5433 is already in use` | Another tunnel or process has it | `dtt ls`, `lsof -i :5433`, or use `auto:5432` |
| `secret "x" is missing` | Not in the keychain | `dtt secret set x --kind env` |
| `the OS keychain is unavailable` | Common on Linux without Secret Service | Install one, or use tunnels without secrets. There is no plaintext fallback |
| `prompt waiting: host key` | First connection to this host | Run on a TTY, or `dtt prompts`, or add the key with `ssh` first |
| `tunnels.toml changed on disk` | Edited by hand | `dtt config reload`, then repeat the change |
| `daemon version 2 is newer than this CLI` | Mixed install | Upgrade `dtt` |
| `ssh was not found` | Not on the daemon's PATH | `dtt tools set ssh /path/to/ssh` |

## Exit codes

Scripts can rely on these.

| Code | Name | Meaning |
|---|---|---|
| 0 | | Success |
| 1 | `failed` | The tunnel or operation failed. See the message and `dtt logs db` |
| 2 | `usage` | Bad arguments, or an unknown or ambiguous tunnel |
| 3 | `needs_secret` | A required secret is missing, or the keychain is unavailable |
| 4 | `needs_user` | A prompt needs a person and no TTY is available (or `--no-input`) |
| 5 | `conflict` | Port in use, or `tunnels.toml` changed on disk |
| 6 | `timeout` | Not ready within the timeout |
| 7 | `daemon` | The daemon cannot be reached or started, or its version is incompatible |
| 8 | `not_found` | A required binary is missing, e.g. `ngrok` |

Keep this table in step with `internal/api` error codes, which MCP also uses.

Use `dtt doctor` to inspect the daemon, binaries and keychain, `dtt tools` for tool paths, and `dtt prompts` for pending SSH questions. `dtt version` never starts a daemon.
