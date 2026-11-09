# Product

**Connect your tunnels in one click.**

## Goals

- Start or stop any tunnel in one click or a few keystrokes, from the main window, the tray or a global quick launch.
- Create and edit tunnels without memorizing flags, starting from an `~/.ssh/config` host, a pasted command or manual input.
- See and manage every tunnel on the machine in one place, including those created outside the app. See [design/control-plane.md](design/control-plane.md).

The desktop app is the main product. Two other ways in feed the same registry:

- **`devt-tunnel`** complements the app: people and agents use it to create managed tunnels in place of plain `ssh`, `ngrok` or `cloudflared` commands.
- **MCP** is a secondary channel for agents to create tunnels. Policy decides what needs a person, who accepts it in the app.

Out of scope: bundling ssh/ngrok/cloudflared, interactive shell sessions. Managing tunnels on other machines is a [later stage](roadmap.md#later-headless-servers).

## Concepts

| Concept | Meaning |
|---|---|
| Tunnel | One supervised process with its configuration. Types: SSH, Cloudflared, ngrok, Command. |
| Forward | One port mapping of an SSH tunnel: `-L` (listen locally), `-R` (listen on the SSH host), `-D` (local SOCKS5). |
| Profile | Named configuration shared by its tunnels, such as the SSH config file (e.g. Work, Home, Client ABC). A tunnel uses at most one profile. |
| Group | Named set of tunnels started and stopped together (e.g. Backend stack). A tunnel can be in many groups, whatever its profile. |
| Secret | API key, tunnel token, SSH key passphrase or env value, kept in the OS keychain and referenced by name. |
| Ephemeral tunnel | A temporary tunnel from `devt-tunnel` or an agent. Not in `tunnels.toml`; removed when it stops; can be saved. |
| Lease | A client's claim to use a tunnel until a set time. A person's stop always wins. |

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

**Create / edit:** New (⌘N), Edit (⌘E) or Duplicate → choose source (SSH config host, pasted command, manual) → configure forwards, flags, profile, groups → review live command preview and checks → Save or Save & start.

Editor checks: resolved binary; host alias in the scanned SSH config; local listen ports (`-L`, `-D`) not used by another tunnel or process; shared ngrok/cloudflared targets reported separately; unsafe SSH flags; missing `-N` and keepalive warnings; required secret present.

## Screens

- **Main:** sidebar with views (All, Running, Favorites), groups with one-click start/stop, search, tunnel list with nested forwards, detail panel (command, PID, uptime, profile, groups, log).
- **Quick launch (⌘K / global):** results grouped as tunnels, SSH hosts without a tunnel, actions; recently used first.
- **Tray:** running tunnels and favorites as check items (native tray menus have no switches); links to quick launch and main window. The tray is optional: without a tray host, closing the main window quits the app instead of leaving tunnels running invisibly.
- **Editor:** type selector, basics (name, profile, groups multi-select, favorite, start on launch, reconnect), type-specific connection, forwards, flags/options, command preview and checks.
- **Profiles:** profile list; per profile: name, description, SSH config file, member tunnels (checking one moves it from its other profile), delete (tunnels are kept).
- **Groups:** group list with running counts; per group: name, description, Start all, Stop all, member tunnels, delete (tunnels are kept).
- **Vault:** secrets with kind, masked value, users, last use; reveal requires OS authentication.

A tunnel with no profile uses the default SSH config. Starting or stopping a group reports every failure together; selecting a group on the main window only filters the list.

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
