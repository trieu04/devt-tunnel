# Phase 4: Paste-command import

**Goal:** turn a pasted `ssh`, `ngrok` or `cloudflared` command into a tunnel draft without dropping arguments, and without storing a pasted secret in the definitions file.

**Depends on:** phases 1 (dialects) and 2 (`ExtraArgs`).

## API

New package `internal/core/importer`, pure, with no OS calls:

```go
type Result struct {
    Tunnel   model.Tunnel
    Warnings []string
    Secrets  []SecretCandidate // values found in the command; never written to the model
}
type SecretCandidate struct {
    Suggested string // secret name
    Value     string
    Kind      string
}
func Parse(line string, d command.Dialect, knownHosts []string) (Result, error)
```

## Preparing the input

Before splitting:

- Join `\` + newline (POSIX) and `^` + newline (Windows). `SplitArgs` would keep the newline.
- Strip a leading prompt: `$ ` or `> `.
- Take a leading `NAME=value` as environment. `NGROK_AUTHTOKEN` and `TUNNEL_TOKEN` become secret candidates. Any other name is a warning, because it would not be applied.
- Reject shell operators outside quotes (`|`, `&&`, `;`, `>`), and fall back to a Command tunnel with the whole line.
- Match the tool by base name without `.exe`, so `/opt/homebrew/bin/ssh` works.
- Any other tool becomes a `TypeCommand` draft with the line as written.

## SSH

Arguments are parsed against getopt's two sets, so that combined flags such as `-NTL 8080:db:5432` work:

- Takes no value: `46AaCfGgKkMNnqsTtVvXxYy`
- Takes a value: `BbcDEeFIiJLlmOoPpQRSWw`

| Input | Result |
|---|---|
| `-N -C -T -A -q -v` | `SSH.Flags` |
| `-L`, `-R` | `Forward`. Forms: `port:host:hostport`, `bind:port:host:hostport`, `[::1]:port:…`. A form with `/` (Unix socket) goes in `ExtraArgs`, since readiness cannot probe it |
| `-D` | `Forward` with `[bind:]port` |
| `-o Key=Value`, `-oKey=Value`, `-o Key Value` | `SSH.Options`, keeping the user's value for any key the builder sets by default |
| `-p`, `-i`, `-l` | `Port`, `Identity`, `User` |
| `-J host` | `Options["ProxyJump"]` |
| `-f` | **Dropped with a warning.** The only argument removed, per architecture.md |
| `-M`, `-S` | Kept in `ExtraArgs`, and the check from phase 3 warns |
| `-F file` | Kept in `ExtraArgs`, with a warning that profiles normally set this |
| Anything else | `ExtraArgs`, in order |

Destination:

- `user@host`, `ssh://user@host:port` and any host with an explicit `-l` or `-p` become manual fields. `ssh` still applies a matching `Host` block, because the name is passed through.
- A bare name goes in `SSH.Host`. If it is not in `knownHosts`, add a warning but do not reject it.
- **Remote command:** arguments after the destination cannot be represented, since the builder places the host last. Return an error suggesting a Command tunnel, rather than silently changing what runs.

Add `-N` only if the original command had it, and let the `-N` check warn. Import should show what was pasted, and the check explains the consequence.

## ngrok

- `ngrok http|tcp|tls <addr>` maps to `Proto` and `Addr`. A bare port becomes `127.0.0.1:<port>`.
- `--domain=x`, `--domain x`, and `--url` with an `https://` host map to `Domain`.
- `--authtoken=x` becomes a secret candidate and is removed from the arguments.
- `--log` and `--log-format` are dropped silently, as the builder sets them.
- Others go to `ExtraArgs`.
- `ngrok start …` and config-file forms become a Command tunnel with a warning.

## cloudflared

| Input | Result |
|---|---|
| `tunnel --url <u>` | Quick tunnel |
| `tunnel run <name>` | Named |
| `tunnel run --token <t> [name]`, `tunnel --token <t> run` | Named, token becomes a secret candidate |
| `--hostname <h>` | `Hostname` |
| `service`, `login`, `access` subcommands | Error: these are not tunnels |
| Others | `ExtraArgs` |

## Secret candidates

- The value stays in memory until the editor saves. `SaveTunnel` does not receive it: the UI calls `SetSecret` first, then saves with only the name (phase 9 provides the name check).
- The editor shows "Found an authtoken. Save it to the Vault as `ngrok-pasted`?" and clears the pasted text after parsing, so the token does not stay on screen.
- Tests assert that no candidate value appears in `Tunnel`, in `ExtraArgs` or in the generated preview.

## Round trip test

The main guarantee from architecture.md. For a table of real-world commands, on both dialects:

1. `Parse(line)` gives a tunnel.
2. `command.Build(tunnel)` gives a spec.
3. Compare argument multisets. The new spec must contain every original argument, except `-f` and moved secrets, and may add only the known defaults (`ExitOnForwardFailure`, `ControlMaster`, `ControlPath`, and ngrok's log flags).

Include `go test -fuzz` on `Parse`, which must never panic on arbitrary input.

## Service

```go
func (s *Service) ImportCommand(line string) (importer.Result, error)
```

It passes `knownHosts` from `SSHHosts` for the active profile. Parsing does not run anything.

## Done when

- The round trip test passes for at least 30 commands, including combined flags, quoted values and Windows paths.
- Every row in the tables above has a test.
- No secret value can reach `tunnels.toml` through import.
