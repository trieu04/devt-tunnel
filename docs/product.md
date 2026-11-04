# Product

## Goals

- Find and start or stop any tunnel in a few keystrokes, from the main window, the tray or a global quick launch.
- Create and edit tunnels without memorizing flags, starting from an `~/.ssh/config` host, a pasted command or manual input.
- Planned: manage every tunnel on the machine from one place, through the app, the `dtt` command line, or agents using MCP. See [design/control-plane.md](design/control-plane.md).

Out of scope: bundling ssh/ngrok/cloudflared, interactive shell sessions, managing tunnels on other machines.

## Concepts

| Concept | Meaning |
|---|---|
| Tunnel | One supervised process with its configuration. Types: SSH, Cloudflared, ngrok, Command. |
| Forward | One port mapping of an SSH tunnel: `-L` (listen locally), `-R` (listen on the SSH host), `-D` (local SOCKS5). |
| Profile | Named group of tunnels (e.g. Work, Home, Client ABC). A tunnel can be in many profiles. |
| Secret | API key, tunnel token, SSH key passphrase or env value, kept in the OS keychain and referenced by name. |
| Ephemeral tunnel (planned) | A temporary tunnel from `dtt` or an agent. Not in `tunnels.toml`; removed when it stops; can be saved. |
| Lease (planned) | A client's claim to use a tunnel until a set time. A person's stop always wins. |

## Tunnel types

| Type | User configures | Runs |
|---|---|---|
| SSH | Host alias from SSH config or manual user/host/port/identity; forwards; flags (`-N -C -T -A -q -v`); `-o` options; extra args | `ssh <flags> -o … -L/-R/-D … <host>` |
| Cloudflared | Named tunnel (name, hostname, token secret) or quick tunnel; local service URL | `cloudflared tunnel run …` or `cloudflared tunnel --url …` |
| ngrok | Protocol, local address, domain, authtoken secret | `ngrok http <addr> --domain=<domain>` |
| Command | Command line, working directory, ready-log pattern; secrets as env vars | The command as written |

Forwards are always displayed as `listen → target` for every kind; the `-L/-R/-D` badge tells which side listens.

## States

`Stopped → Starting → Running`. `Running → Reconnecting → Running` when auto-reconnect is on and the process exits unexpectedly. `Starting → Failed` on error. A tunnel whose required secret is missing shows `Needs token` and cannot start; its action opens the Vault. Where no OS keychain is available, it shows `Keyring unavailable` instead.

## Flows

**Launch:** open the app, the tray or global quick launch → search by name, host or port → Enter starts or stops → if a secret is missing, add it in the Vault and the start retries → running: copy address, view log. No search result offers "create tunnel from host" for matching SSH config hosts.

**Create / edit:** New (⌘N), Edit (⌘E) or Duplicate → choose source (SSH config host, pasted command, manual) → configure forwards, flags, profiles → review live command preview and checks → Save or Save & start.

Editor checks: resolved binary; host alias in the scanned SSH config; local listen ports (`-L`, `-D`) not used by another tunnel or process; shared ngrok/cloudflared targets reported separately; unsafe SSH flags; missing `-N` and keepalive warnings; required secret present.

## Screens

- **Main:** sidebar filters (All, Running, Favorites, by type), active-profile selector, search, tunnel list with nested forwards, detail panel (command, PID, uptime, profiles, log).
- **Quick launch (⌘K / global):** results grouped as tunnels, SSH hosts without a tunnel, actions; recently used first.
- **Tray:** running tunnels and favorites as check items (native tray menus have no switches); links to quick launch and main window. The tray is optional: without a tray host, closing the main window quits the app instead of leaving tunnels running invisibly.
- **Editor:** type selector, basics (name, profiles multi-select, favorite, start on launch, reconnect), type-specific connection, forwards, flags/options, command preview and checks.
- **Profiles:** profile list; per profile: name, description, SSH config file, behavior on activation, member tunnels with per-tunnel auto-start, delete (tunnels are kept).
- **Vault:** secrets with kind, masked value, users, last use; reveal requires OS authentication.

## Profiles

- Tunnel ↔ profile is many-to-many. One profile (or "All profiles") is active at a time; it filters the main list and the tray.
- On activation a profile either only filters, or also starts its auto-start tunnels. Optionally it stops running tunnels that are not members.
- Each profile may use its own SSH config file.

## Shortcuts

On Windows and Linux, ⌘ maps to Ctrl.

| Key | Action |
|---|---|
| ⌥Space (macOS), Ctrl+Alt+Space (Windows, Linux) | Global quick launch; a registration conflict is reported in the UI |
| ⌘K | Quick launch in main window |
| ↵ / ⌘↵ (quick launch) | Start/stop / start and copy address |
| ⌘N / ⌘E | New / edit tunnel |
| ⌘C / ⌘L (quick launch) | Copy address / open log |
| ⌘⇧V | Create tunnel from pasted command |
| ⌘Q | Stop all and quit |
