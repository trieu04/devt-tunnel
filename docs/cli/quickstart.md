# Quick start

Use `dtt` in place of plain `ssh`, `ngrok` or `cloudflared` commands, so that tunnels you or your agents create are tracked and can be managed from the desktop app.

## Install the CLI

The desktop packages bundle `dtt`; on macOS, use **Install command-line tool** in Settings. To build it with Go 1.27.3 or newer:

```bash
CGO_ENABLED=0 go build -o bin/dtt ./cmd/dtt
install -m 755 bin/dtt "$HOME/.local/bin/dtt"
```

## Save and run a tunnel

Reuse a host from `~/.ssh/config`. This is the supervised equivalent of `ssh -N -L 5433:127.0.0.1:5432 server1`:

```bash
dtt add ssh staging-db --host server1 -L 5433:127.0.0.1:5432
dtt start staging-db
dtt ls
dtt addr staging-db --port
dtt logs staging-db -n 20
dtt stop staging-db
```

- The daemon starts on demand and is shared with the desktop app. `dtt daemon stop`, `restart` and quitting the app stop all its tunnels.
- References accept an exact id, exact name or unique id prefix.
- SSH questions appear inline on a terminal. Without one, or with `--no-input`, the command exits 4; answer with `dtt prompts` or the app.
- Secret values are accepted only from a hidden prompt or `dtt secret set --stdin`.

## One-off tunnels

Use the command you already know. `dtt` runs it under supervision and stops it on Ctrl-C:

```bash
dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1
```

Or use the short form, which stays in the background until `dtt stop`, or until an explicit `--ttl` (maximum 8h) expires:

```bash
dtt forward server1 5433:5432
```

Keep it under a name, and pass its address to another program:

```bash
dtt save tmp-server1 --name staging-db --profile work
```

```bash
psql "host=127.0.0.1 port=$(dtt addr staging-db --port) dbname=app"
```

Expose a local service publicly. `expose` always warns on stderr that the service will be public:

```bash
dtt expose 3000 --via cloudflared --ttl 15m
```

See who holds what, and review agent requests:

```bash
dtt leases
dtt approvals
dtt audit --since 1h
```

`approvals approve <id>` needs a terminal and the code shown by `approvals`.

## Shell completion and offline help

```bash
dtt completion zsh > _dtt
dtt docs
dtt help add ssh
dtt man --dir ./man
```

`dtt completion` also supports bash, fish and powershell. Tab completion never starts a daemon. The [command reference](reference/dtt.md) is generated from the command definitions.
