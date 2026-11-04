# Phase 6: Tunnel editor

**Goal:** create, edit, duplicate and delete tunnels of all four types, from an SSH host, a pasted command or manual input, with a live preview and checks.

**Depends on:** phases 2, 3, 4, 5. **Mockup:** board "Tạo / sửa tunnel". **Decisions:** D4, D5.

## 6.1 Draft state

`screens/EditorScreen.tsx` owns one `model.Tunnel` draft in a reducer.

- The draft has the same JSON shape Go uses, so no mapping layer is needed. Generated binding types are the form's types.
- Sources of a draft: `NewDraft(kind)`, an existing tunnel, `DuplicateDraft(id)`, `ImportCommand(line)`, or a host chosen in the quick launch (phase 7).
- Switching type keeps name, profiles and flags such as favorite and reconnect. It clears other type sections, with one undo available until the next edit.
- "Unsaved changes" compares draft to original with a stable deep equal. Leaving asks for confirmation.

## 6.2 Live preview and checks

- `useDraftEvaluation(draft)`: debounces 150 ms, calls `EvaluateDraft`, ignores out-of-order responses using a request counter.
- The UI never builds a command or decides a check. This is the preview-equals-execution rule from architecture.md.
- Show the last good preview, dimmed, while `buildError` is present, with the error below.
- Copy on the preview. "Open in terminal" is cut (D4).
- Text beneath: "Updates as you edit. This is exactly what devt-tunnel runs."
- `Finding.Field` places a message under its input. All findings also appear in the Checks list with ok, warning or error icons.

## 6.3 Layout

Sections, in order, from the mockup:

**Header:** breadcrumb, title, Unsaved badge, Cancel, Save, Save & start.

**Source:** segmented Paste a command / Fill form. Paste has a textarea and a Parse button. Warnings appear under it, and a successful parse fills the form and switches to Fill form.

**Type:** SSH, Cloudflared, ngrok, Command. Locked when editing, because the id and references stay but the settings would not map. Duplicating is the way to change type.

**Basics:** name (required), profiles multi-select with "Manage profiles" link, Favorite, Start when devt-tunnel opens, Reconnect automatically.

## 6.4 SSH section

- Note: "Runs your system ssh, so agent, ProxyJump and Match blocks keep working."
- Segmented: **Host from SSH config** / **Manual (user@host)**. Switching clears the other side's values, since `SSH.Host` takes priority in `buildSSH`.
- **Host select:** filterable list from `SSHHosts`. Below it: "12 hosts read from ~/.ssh/config · Reload". Show the file actually used for the selected profile, and warnings from the scanner. After selecting, show resolved HostName, User, Port, IdentityFile, ProxyJump from `ResolveSSHHost`.
- **Manual:** User, Host, Port, Identity file (with a file picker through a new `Service.PickFile`, using Wails `Dialog.OpenFile`).
- **Key passphrase:** "Use ssh-agent or ask" (D5), or a Vault secret, with "Add new…" opening the inline secret form (6.8).
- **Forwards:** table with buttons `+ -L local`, `+ -R remote`, `+ -D SOCKS`. Row fields: type, bind address, port, target host, target port, delete. `-D` shows "SOCKS5 proxy through the SSH host". The bind default is `127.0.0.1`. Bind and Target are composed into the model's strings by `lib/forward.ts`, with tests: splitting `[::1]:80` and a bare port back into fields must round trip.
- **Flags:** `-N -C -T -A -q -v` with a description each. `-o ServerAliveInterval` as a number, `-o ExitOnForwardFailure` as yes/no, other options as key-value rows. Extra arguments as text.
- Stored option values not in the known list stay intact in the key-value rows, so that editing cannot delete them.

## 6.5 Other types

**ngrok:** Protocol, Local address, Domain, Authtoken (Vault select), Extra arguments.

**Cloudflared:** segmented Named tunnel / Quick tunnel. Named: tunnel name, public hostname, token (Vault select), local service. Quick: local service only, with "random trycloudflare.com URL". Note that `Validate` accepts a named tunnel with neither URL nor token, and that is valid for tunnels configured by file.

**Command:** command line (multi-line), working directory with a folder picker, Ready-when-log-matches (a regex, validated by `EvaluateDraft` through `Build`), and secret environment rows of `NAME → Vault secret`.

The mockup's "Use `$VAULT_NAME`" hint implies a magic syntax. The model uses explicit `secret_envs`, so offer those rows instead. As a convenience, if the line references `$FOO` and a Vault entry named `FOO` exists but has no row, show an "Add" suggestion.

## 6.6 Saving

- Save: `SaveTunnel(draft, false)`, then return to the list with the tunnel selected. Save & start: `SaveTunnel(draft, true)`.
- Only `buildError` and validation errors block Save. A missing secret or binary does not, because a tunnel can be defined before its secret or tool exists. Save & start is blocked by any `error` finding.
- **Conflict (`ErrConflict`):** dialog "tunnels.toml changed on disk". Options: **Reload and reapply** (runs `Reload`, then saves the draft over the new file, which merges at tunnel level) or **Keep editing**. Never overwrite silently.
- Editing a running tunnel: Save leaves it running on old settings, and the row shows the Stale badge. Save & start restarts it.

## 6.7 Delete

Editor footer and row context. Dialog names the tunnel and says whether it is running. Removes it from profiles' autostart, and removes the `LastUsed` entry. Referenced secrets stay in the Vault.

## 6.8 Secret select

`components/SecretSelect.tsx`, shared by SSH, ngrok, cloudflared and Command.

- Options come from `ListSecrets` (phase 9). Until that merges, use a list derived from names referenced in the config.
- Shows the name and present or missing state, not a partial value (D3).
- "Add new…" opens a small form for name and value that calls `SetSecret`. Password field, no autofill, cleared after saving.
- On import, secret candidates pre-fill this form.

## 6.9 Shortcuts

⌘S save, ⌘↵ Save & start, Esc cancel with the unsaved check, and ⌘N, ⌘E, ⌘⇧V wired from phase 5. ⌘⇧V opens paste mode and, if the clipboard text starts with a known tool, fills the textarea.

## Tests

- Reducer: every action, type switching, forward composition.
- Component: editing a stored tunnel with unknown options and saving sends them back unchanged.
- Component: out-of-order preview responses are ignored; conflict dialog appears on `ErrConflict`.
- E2E on macOS native: new SSH tunnel from a config host → Save & start → reaches Running with a real `ssh`, against the private `sshd` setup in `internal/e2e` or a real host.

## Done when

- The v0.2 exit test passes: a tunnel is created from an SSH host and from a pasted command without opening the file.
- All four types can be created, saved, reloaded and started, with the file valid after each.
- README Status and "Until the editor exists" text are updated.
