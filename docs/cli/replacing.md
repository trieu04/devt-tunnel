# Replacing traditional commands

Use familiar SSH and public-tunnel commands through the daemon, with supervised readiness, logs and ownership. Background forms keep a detached person lease until you stop/revoke it, or until an explicit `--ttl` expires.

| You used to run | Now run | What you gain |
|---|---|---|
| `ssh -N -L 5433:127.0.0.1:5432 server1` | `dtt forward server1 5433:5432` | Reconnects, status, logs, port-conflict checks. |
| `ssh -f -N -L 5433:db:5432 server1` | `dtt forward server1 5433:db:5432` | Background with a known PID and a clean stop. `-f` is never used. |
| `ssh -N -R 8080:127.0.0.1:3000 server1` | `dtt forward server1 --remote 8080:3000` | Same, for the remote side. |
| `ssh -N -D 1080 server1` | `dtt forward server1 --socks 1080` | Same, for SOCKS5. |
| `autossh -M 0 -N -L ...` | `dtt forward server1 auto:5432` | Reconnect with backoff is the default. |
| `ngrok http 3000` | `dtt expose 3000 --via ngrok` | Token from the keychain; address printed when ready. |
| `cloudflared tunnel --url http://localhost:3000` | `dtt expose 3000 --via cloudflared` | Prints the public URL when ready. |
| `cloudflared tunnel run prod` | `dtt add cloudflared prod --tunnel-name prod --secret cf-token`, then `dtt start prod` | Token stays out of shell history |
| `ps aux \| grep ssh`, `lsof -i :5433` | `dtt ls`, `dtt show db` | All tunnels and their owners in one list |
| `kill <pid>` | `dtt stop db` | Stops the whole process tree |
| Any of the above, unchanged | `dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1` | Nothing to learn. |

Keep using `ssh` directly for interactive sessions, `scp` and `rsync`. `dtt` handles tunnels only.

`dtt run` accepts `ssh`, `ngrok` and `cloudflared` through the same importer as the app's paste dialog. Unrecognized flags go to `extra_args`, so nothing is dropped. Other programs need `dtt add command worker --line ./worker --ready-log Ready`, because a command's readiness cannot be guessed.
