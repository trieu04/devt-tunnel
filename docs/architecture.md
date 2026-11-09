# Architecture

Describes intended behavior. Stack, per-OS details and code layout are in [platform.md](platform.md); the README lists what is implemented so far.

## Control plane

Phase 15 adds typed readiness errors, shared endpoint lists and loopback port checks; native-window port-warning acceptance is pending. Phase 14 extracted the shared `internal/api` types and GUI-independent `internal/engine`; phase 16 added the headless daemon/API and lifecycle CLI; phase 17 moved the GUI host to that API with native-window acceptance pending. An on-demand per-user daemon owns the registry, supervisor and prompts, and "the app" below means the daemon. The GUI, the `dtt` CLI and the planned MCP server are clients of one local API. See [design/control-plane.md](design/control-plane.md), [design/cli.md](design/cli.md) and [design/mcp.md](design/mcp.md).

## Process model

- Each running tunnel is one child process of the system binary (`ssh`, `cloudflared`, `ngrok`) or of the user's command, spawned and supervised by the daemon. SSH tunnels never use `-f`; the daemon owns the lifecycle.
- The command preview in the editor is generated from the same argv the daemon spawns, so preview and execution cannot diverge.
- Ready detection: SSH runs with `-o ExitOnForwardFailure=yes` and is ready once every local listen port (`-L`, `-D`) accepts connections; `-R` relies on the process staying alive. Cloudflared, ngrok and Command tunnels are ready when a log line matches their pattern (e.g. the public URL).
- stdout/stderr go to a per-tunnel in-memory log ring buffer shown in the detail panel.
- Unexpected exit with auto-reconnect on → restart with backoff (starting at 5 s). Stop ends the whole process tree: SIGTERM to the process group, then SIGKILL after a timeout on Unix; `TerminateJobObject` on Windows. Quit stops all tunnels.
- Each started process is recorded with PID and start time in the state file. A record that still matches on the next launch is an orphan from a crash, and the app offers to stop it.

## SSH integration

- Tunnels built from a config host pass the alias to `ssh`, so `Include`, `Match`, `ProxyJump`, `IdentityFile` and the agent behave exactly as in a terminal.
- A bounded parser scans `Host` and `Include` directives for literal aliases; it skips wildcard and negated patterns and does not interpret connection settings. `ssh -G <alias>` runs only after explicit selection to resolve HostName, User, Port, IdentityFile and ProxyJump with OpenSSH's full `Match`/`Include` semantics.
- A profile's SSH config file is passed with `-F`; note that `-F` replaces the default config instead of adding to it.
- Supervised tunnels pass `-o ControlMaster=no -o ControlPath=none` so that the forward belongs to the process the daemon supervises.
- All SSH prompts go through the `devt-tunnel-askpass` helper (`SSH_ASKPASS` with `SSH_ASKPASS_REQUIRE=force`), which forwards them to the daemon over a local socket with a per-spawn token. Passphrase prompts are answered from the Vault when the tunnel names a secret; every other prompt (host key, password, OTP) is queued for a user client. The passphrase never appears in argv or env.

## Secrets

- Stored only in the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service). Definition files hold secret names, never values. There is no plaintext fallback: without a keychain, tunnels that need a secret show `Keyring unavailable`.
- Injected at spawn as environment variables (`NGROK_AUTHTOKEN`, `TUNNEL_TOKEN`, or a custom name); never logged, never shown in the command preview.
- Revealing a value requires OS authentication. Where the desktop offers no suitable prompt (Linux for now), values can be replaced but not revealed. Deleting a secret that a tunnel uses puts that tunnel in `Needs token`.

## Storage

- Definitions: `tunnels.toml` (tunnels, profiles, groups) in `~/.config/devt-tunnel/` on macOS, `$XDG_CONFIG_HOME/devt-tunnel/` on Linux and `%APPDATA%\devt-tunnel\` on Windows. Kept free of runtime state so it stays diffable and shareable.
- Runtime state lives in `state.json` under the OS state directory; crash process records remain in `processes.json`. Per-machine tool paths, shortcut and shell live in `settings.toml` beside the definitions. Config writes detect outside changes, keep `tunnels.toml.bak`, validate before replacement and write atomically; hand-written comments are not preserved.

```toml
[[profile]]
id = "work"
name = "Work"
ssh_config = "~/.ssh/config"

[[group]]
id = "backend"
name = "Backend"

[[tunnel]]
id = "srv1"
name = "SSH Server 1"
type = "ssh"                      # ssh | cloudflared | ngrok | command
profile = "work"                  # at most one; membership lives on the tunnel
groups = ["backend"]              # any number
favorite = true
start_on_launch = false
reconnect = true

[tunnel.ssh]
host = "server1"                  # config alias; manual tunnels set user/hostname/port/identity instead
flags = ["-N", "-C"]
options = { ServerAliveInterval = "30", ExitOnForwardFailure = "yes" }
extra_args = ""

[[tunnel.forward]]
kind = "L"                        # L | R | D
bind = "127.0.0.1:4053"
target = "192.168.0.24:8000"      # omitted for D

[[tunnel]]
id = "web"
name = "Ngrok"
type = "ngrok"
groups = ["backend"]

[tunnel.ngrok]
proto = "http"
addr = "127.0.0.1:5555"
domain = "example.com"
secret = "ngrok-personal"
```

## Paste-command import

Parses `ssh`, `ngrok` and `cloudflared` argv into the model. Recognized flags and `-o` options map to fields; anything unrecognized goes to `extra_args` so a round trip never drops arguments.

## Network recovery

SSH remains responsible for detecting dead connections; new SSH drafts set `ServerAliveInterval=30`, and the supervisor reconnects after exit. A system wake event or a wall-clock jump interrupts reconnect backoff, but the app does not add a separate network-change listener.

## Decisions

- **System binaries over an embedded SSH library:** full `~/.ssh/config` and agent compatibility at the cost of depending on installed versions.
- **No `-f`:** background mode hides the PID and exit status the supervisor needs.
- **Keychain-only secrets:** the definitions file can be committed or shared safely.
- **`listen → target` for all forward kinds:** one reading direction; the badge carries the side.
- **Profiles configure, groups act:** a tunnel has at most one profile, so its SSH config never depends on app state. Starting and stopping sets of tunnels belongs to groups, which are many-to-many and have no effect on how a tunnel is built.
- **No quick-phrase aliases:** quick launch search over name, host and port covers the use case with less to maintain.
- **Wails v3 with a Go core:** first-party tray and global shortcuts, and the core stays free of GUI dependencies. See [platform.md](platform.md).
- **On-demand daemon, GUI as a client:** the CLI and agents need tunnels with no desktop and one shared registry, so this replaces "one process, no daemon" (2026-11-04). ⌘Q still stops all tunnels. See [design/control-plane.md](design/control-plane.md#why-a-daemon).
- **Askpass forwards prompts:** `SSH_ASKPASS_REQUIRE=force` routes every prompt to the helper, so it must not answer them all with a stored passphrase.
- **No sandboxed distribution:** Mac App Store, Flatpak and strict Snap conflict with spawning host binaries.
- **No v1 import/export format:** copying `tunnels.toml` is the sharing mechanism.

## Open questions

- Windows and Linux native behavior still requires the real-desktop validation matrix in [platform.md](platform.md).
