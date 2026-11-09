# Scripts

Open a tunnel for one job, and clean up even if the job fails:

```bash
#!/bin/sh
dtt forward staging auto:5432 --ttl 15m --json > tunnel.json || exit
id=$(jq -r '.tunnels[0].id' tunnel.json)
port=$(dtt addr "$id" --port)
trap 'dtt stop "$id"' EXIT
pg_dump -h 127.0.0.1 -p "$port" app > app.sql
```

The JSON result contains `tunnels[0].id` and the allocated endpoints. Its detached lease survives CLI exit; `--ttl 15m` is a safety limit if cleanup cannot run. Without `--ttl`, a background tunnel stays up until explicitly stopped or revoked. `--dry-run` prints an evaluated command and findings without starting anything; because auto allocation only happens at start, use a concrete listen port for a dry-run preview.

Start a saved tunnel only if needed. `start` is idempotent and returns 0 if it is already running:

```bash
dtt start db && psql -h 127.0.0.1 -p "$(dtt addr db --port)" app
```

Use `--json` for stable, versioned results (`apiVersion`), not human tables. Diagnostics and JSON errors go to stderr. `logs --json -f` and `audit --json -f` emit one versioned object per batch. Retained log lines have sequence numbers but no timestamps, so a positive `--since` currently exits 2. Pass `--no-input` in automation and `--yes` when deleting a tunnel without a terminal. See [Troubleshooting](troubleshooting.md) for exit codes.
