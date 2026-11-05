# Replacing traditional commands

The `run`, `forward` and `expose` replacements below are **available after phase 19**, not in the current CLI. Their rows carry `phase19` markers in the Markdown source for the example parse check. They remain here as the planned migration path. Unmarked rows work today.

| You used to run | Now run | What you gain |
|---|---|---|
| `ssh -N -L 5433:127.0.0.1:5432 server1` | `dtt forward server1 5433:5432` | Reconnects, status, logs, port-conflict checks. **Available after phase 19.** <!-- phase19 --> |
| `ssh -f -N -L 5433:db:5432 server1` | `dtt forward server1 5433:db:5432` | Background with a known PID and a clean stop. `-f` is never used. **Available after phase 19.** <!-- phase19 --> |
| `ssh -N -R 8080:127.0.0.1:3000 server1` | `dtt forward server1 --remote 8080:3000` | Same, for the remote side. **Available after phase 19.** <!-- phase19 --> |
| `ssh -N -D 1080 server1` | `dtt forward server1 --socks 1080` | Same, for SOCKS5. **Available after phase 19.** <!-- phase19 --> |
| `autossh -M 0 -N -L ...` | `dtt forward ...` | Reconnect with backoff is the default. **Available after phase 19.** <!-- phase19 --> |
| `ngrok http 3000` | `dtt expose 3000 --via ngrok` | Token from the keychain; address printed when ready. **Available after phase 19.** <!-- phase19 --> |
| `cloudflared tunnel --url http://localhost:3000` | `dtt expose 3000 --via cloudflared` | Prints the public URL when ready. **Available after phase 19.** <!-- phase19 --> |
| `cloudflared tunnel run prod` | `dtt add cloudflared prod --tunnel-name prod --secret cf-token`, then `dtt start prod` | Token stays out of shell history |
| `ps aux \| grep ssh`, `lsof -i :5433` | `dtt ls`, `dtt show db` | All tunnels and their owners in one list |
| `kill <pid>` | `dtt stop db` | Stops the whole process tree |
| Any of the above, unchanged | `dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1` | Nothing to learn. **Available after phase 19.** <!-- phase19 --> |

Keep using `ssh` directly for interactive sessions, `scp` and `rsync`. `dtt` handles tunnels only.

`dtt run` accepts `ssh`, `ngrok` and `cloudflared` through the same importer as the app's paste dialog. Unrecognized flags go to `extra_args`, so nothing is dropped. Other programs need `dtt add command worker --line ./worker --ready-log Ready`, because a command's readiness cannot be guessed.
