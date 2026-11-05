# Quick start

## Install the headless CLI

From a checkout with Go 1.27.3 or newer, build and install the single headless binary (no Node, GTK, WebView or display required):

```bash
CGO_ENABLED=0 go build -o bin/dtt ./cmd/dtt
mkdir -p "$HOME/.local/bin"
install -m 755 bin/dtt "$HOME/.local/bin/dtt"
```

Add `$HOME/.local/bin` to `PATH`. On Windows, build with `go build -o bin/dtt.exe ./cmd/dtt` and place it in a directory on `PATH`. Signed CLI packages and installer PATH integration are planned for phase 21.

## Save and run a tunnel today

Reuse a host from `~/.ssh/config`. This is the supervised equivalent of `ssh -N -L 5433:127.0.0.1:5432 server1`:

```bash
dtt add ssh staging-db --host server1 -L 5433:127.0.0.1:5432
dtt start staging-db
dtt ls
dtt show staging-db
dtt addr staging-db --port
dtt logs staging-db -n 20
dtt stop staging-db
```

The daemon starts on demand and is shared with the desktop app. Tunnel references accept an exact ID, exact name or unique ID prefix. An ambiguous reference exits 2. `start` is idempotent; saved definitions survive `stop`. `--no-daemon-start` only connects to an existing daemon. `dtt daemon stop` and `restart` also stop all daemon-owned tunnels. The GUI stops the shared daemon and its tunnels when it quits.

First-time SSH host-key questions appear inline on a terminal. Without a terminal or with `--no-input`, a question exits 4 rather than hanging; inspect it with `dtt prompts` or the app. Secret values are accepted only from a hidden prompt or `dtt secret set --stdin`, not flags or environment variables.

## One-off tunnels

Use the command you already know. `dtt` parses it, runs it under supervision, and stops it when you press Ctrl-C:

```bash
dtt run -- ssh -N -L 5433:127.0.0.1:5432 server1
```

Or use the short form, which stays in the background until you close it with `dtt stop` or revoke its lease. Background commands use a detached person lease: it survives CLI exit and has no expiry unless you pass `--ttl` (maximum 8h). `run` defaults to the foreground with a 30m lease; `--detach` selects the background behavior.

```bash
dtt forward server1 5433:5432
```

```bash
dtt ls
```

```text
ID           TYPE         STATE     ENDPOINT              OWNER      EXPIRES
tmp-server1  ssh          running   127.0.0.1:5433        cli
db           ssh          running   127.0.0.1:15432       pinned
web          ngrok        stopped   example.ngrok.app
api-test     cloudflared  running   https://x.trycloudflare.com  claude-code  23m
```

Save it under a name to reuse later, then start it by name:

```bash
dtt save tmp-server1 --name staging-db --profile work
```

```bash
dtt start staging-db
```

Pass its address to another program:

```bash
psql "host=127.0.0.1 port=$(dtt addr staging-db --port) dbname=app"
```

Expose a local service publicly (the provider is always explicit because `default_expose` is not implemented):

```bash
dtt expose 3000 --via cloudflared --ttl 15m
dtt leases
dtt approvals
dtt audit --since 1h
```

`expose` always warns on stderr that the service will be public. `approvals approve <id>` (also `approve <id>`) requires a terminal and asks for the code shown by `approvals`; `approvals deny <id>` (also `deny <id>`) can run without a terminal. Saving preserves the running process and its lease, rather than pinning it.

## Shell completion and offline help

Generate a script for your shell and load it from the shell's startup configuration:

```bash
dtt completion bash > dtt.bash
source ./dtt.bash
dtt completion zsh > _dtt
dtt completion fish > dtt.fish
dtt completion powershell > dtt.ps1
```

For Zsh, place `_dtt` in a directory on `$fpath` and enable `compinit`; for Fish, install `dtt.fish` under `~/.config/fish/completions/`. PowerShell users can dot-source `dtt.ps1` from their profile. Dynamic tunnel IDs/names and profile names come only from an already-running daemon, with a 250ms limit and no diagnostics on failure. Tab completion never starts a daemon.

```bash
dtt docs
dtt docs replacing
dtt help add ssh
dtt man --dir ./man
man ./man/dtt.1
```

Guides are embedded in the binary. The [command reference](reference/dtt.md) is generated from Cobra definitions and committed. From the repository root, `go run ./internal/cli/gen` regenerates `docs/cli/reference/*.md` and section 1 man pages in ignored `bin/man/`; only the reference Markdown is committed. Release packaging can install those man pages into `share/man/man1`. Both outputs disable auto-generated date/history tags, and the man header date is fixed to the interface contract date for reproducibility. `dtt man` generates the same pages offline, defaulting to the current directory.

Phases 18–19 implement saved and ephemeral tunnels, lifecycle, prompts, profiles, secrets, leases, approvals, audit, orphans, configuration, tools and setup. Daemon login installation and MCP are not implemented yet. Native/Linux no-GTK exit acceptance has not yet been run.
