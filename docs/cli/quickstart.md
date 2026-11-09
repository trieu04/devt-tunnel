# Quick start

Use `devt-tunnel` in place of plain `ssh`, `ngrok` or `cloudflared` commands, so that tunnels you or your agents create are tracked and can be managed from the desktop app.

## Install the CLI

The desktop packages bundle `devt-tunnel`; on macOS, use **Install command-line tool** in Settings. To build it with Go 1.27.3 or newer:

```bash
CGO_ENABLED=0 go build -o bin/devt-tunnel ./cmd/devt-tunnel
install -m 755 bin/devt-tunnel "$HOME/.local/bin/devt-tunnel"
```

## Save and run a tunnel

Reuse a host from `~/.ssh/config`. This is the supervised equivalent of `ssh -N -L 5433:127.0.0.1:5432 server1`:

```bash
devt-tunnel add ssh staging-db --host server1 -L 5433:127.0.0.1:5432
devt-tunnel start staging-db
devt-tunnel ls
devt-tunnel addr staging-db --port
devt-tunnel logs staging-db -n 20
devt-tunnel stop staging-db
```

- The daemon starts on demand and is shared with the desktop app. `devt-tunnel daemon stop`, `restart` and quitting the app stop all its tunnels.
- References accept an exact id, exact name or unique id prefix.
- SSH questions appear inline on a terminal. Without one, or with `--no-input`, the command exits 4; answer with `devt-tunnel prompts` or the app.
- Secret values are accepted only from a hidden prompt or `devt-tunnel secret set --stdin`.

## One-off tunnels

Use the command you already know. `devt-tunnel` runs it under supervision and stops it on Ctrl-C:

```bash
devt-tunnel run -- ssh -N -L 5433:127.0.0.1:5432 server1
```

Or use the short form, which stays in the background until `devt-tunnel stop`, or until an explicit `--ttl` (maximum 8h) expires:

```bash
devt-tunnel forward server1 5433:5432
```

Keep it under a name, and pass its address to another program:

```bash
devt-tunnel save tmp-server1 --name staging-db --profile work
```

```bash
psql "host=127.0.0.1 port=$(devt-tunnel addr staging-db --port) dbname=app"
```

Expose a local service publicly. `expose` always warns on stderr that the service will be public:

```bash
devt-tunnel expose 3000 --via cloudflared --ttl 15m
```

See who holds what, and review agent requests:

```bash
devt-tunnel leases
devt-tunnel approvals
devt-tunnel audit --since 1h
```

`approvals approve <id>` needs a terminal and the code shown by `approvals`.

## Shell completion and offline help

```bash
devt-tunnel completion zsh > _devt-tunnel
devt-tunnel docs
devt-tunnel help add ssh
devt-tunnel man --dir ./man
```

`devt-tunnel completion` also supports bash, fish and powershell. Tab completion never starts a daemon. The [command reference](reference/devt-tunnel.md) is generated from the command definitions.
