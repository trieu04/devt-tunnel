# Phase 18: `dtt` command line

**Goal:** all of [cli.md](../design/cli.md), except the commands that depend on phase 19, and documentation that cannot drift from the code.

**Depends on:** 16. Can run alongside 17 and 19.

## 18.1 Foundation

- `internal/cli` with `cobra`: root command, global flags, output helpers, and reference resolution (exact id, exact name, unique prefix).
- One function maps API error kinds to the exit codes in cli.md, and one writes `--json` errors.
- `IsTerminal` decides colors, tables and prompts. `--no-input` and `NO_COLOR` are honored.

## 18.2 Commands, in shipping order

Each group can be one pull request.

| Group | Commands |
|---|---|
| Read | `ls` `show` `addr` `logs` `check` `hosts` `doctor` `version` |
| Lifecycle | `start` `stop` `restart` `wait` |
| Prompts | Inline TTY prompts for `start`, plus `prompts`, `prompts answer` |
| Define | `add ssh\|ngrok\|cloudflared\|command` `import` `edit` `set` `dup` `rm` |
| Organize | `profile *` `secret *` `orphans *` `config *` `tools *` |
| Setup | `daemon *` on `cobra` `completion` `docs` `man` |
| Depends on phase 19 | `run` `forward` `expose` `save` `leases` `approvals` `audit` |

Rules for each:

- `secret set` reads only a hidden prompt or `--stdin`. Add a test that no flag accepts a value.
- `--dry-run` uses `tunnel.evaluateDraft`, so the printed command is the one that would run.
- `start` is idempotent.
- `rm` without a TTY needs `--yes`.

## 18.3 Documentation

| Task | Detail |
|---|---|
| Help text | Every command has `Long` and `Example`. Where a traditional command exists, the help shows its equivalent |
| Reference | `go run ./internal/cli/gen` writes `docs/cli/reference/*.md` and man pages |
| Guides | Move the quick start, replacement table, scripts and troubleshooting from `design/cli.md` to `docs/cli/`, embedded for `dtt docs` |
| README | An install and usage section, and links |

CI checks, each failing the build:

- Regenerating the reference leaves no diff.
- Each `dtt ...` line in `docs/cli/*.md` parses against the command tree.
- The exit code table matches `internal/api`.
- No command lacks `Long` or `Example`.

## Tests

- Golden files for `--json` output and for tables.
- A table test for every exit code, against a real daemon on a temporary socket.
- Reference resolution: exact, prefix, ambiguous and missing.
- Non-TTY behavior: a pending prompt exits 4 and does not hang.

## Exit test

On a Linux machine with no GTK and no display, using a single binary: install `dtt`, `dtt add ssh`, `dtt start` with a host-key prompt on the TTY, connect through the forward, `dtt logs`, `dtt stop`. Each command in the guides has been run at least once.
