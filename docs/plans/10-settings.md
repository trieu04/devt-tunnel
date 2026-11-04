# Phase 10: Settings

**Goal:** the Settings screen around the Vault: binaries, SSH and config files, shortcuts, and general information.

**Depends on:** phases 2, 3, 5. **Mockup:** "Vault" board's sidebar: General, Vault, Profiles, SSH & config files, Binaries, Shortcuts. Only the Vault page was mocked, so the other pages below are proposals.

## 10.1 Screen

`screens/SettingsScreen.tsx`: left navigation with the six entries. Profiles links to the Profiles screen rather than duplicating it. Reached from the sidebar and from ⌘, on macOS, Ctrl+, elsewhere.

## 10.2 Binaries

One card per tool (`ssh`, `ngrok`, `cloudflared`), from `Service.Tools()`:

- Resolved path, version, and how it was found ("from login shell"). Not found: a clear state and a link to the tool's install page, opened with the OS browser.
- Path override: text and file picker, plus Clear. Validate on save by running the version command, showing the output or error.
- Button to refresh.

```go
func (s *Service) SetToolPath(tool, path string) ([]ToolInfo, error)
```

It writes `settings.toml`, then calls `Tools.SetOverrides`, then `notify`, because checks and states may change. Since an override is an arbitrary executable that the app will run, require an absolute path to an existing executable file.

Also show the shell for Command tunnels. On Windows offer `cmd` or PowerShell, explicitly, per platform.md. This requires `platform.Shell` to read the setting, and `configure` in `process_windows.go` only special-cases `cmd.exe`, so test PowerShell quoting.

## 10.3 SSH and config files

- Definitions path with Reveal in file manager (moved from the main window's "Edit tunnels.toml"), and Reload. Shows the current load error, if any.
- Backup: "Restore previous version" from `tunnels.toml.bak`, with a confirmation.
- Default SSH config path, and per-profile overrides listed read-only with links.
- Host list: count, files read, scanner warnings, Reload.
- State directory path, for support.

## 10.4 Shortcuts

- Current global shortcut with its registration state. Key capture input to change it, plus Reset to default.

```go
func (s *Service) SetShortcut(accel string) error
```

- `registerShortcut` in `desktop.go` handles only one startup registration. Change it to `Unregister` the old one and `Register` the new one. Failure keeps the old binding and returns the conflict as an error (platform.md: conflicts are surfaced).
- `IsRegistered` and `GetAll` exist on Wails's manager, but they report only this app's registrations, so a conflict is only known from `Register`'s error. Check on each OS that it does return one, as this is check 1 of the matrix.
- On Wayland, the portal or compositor decides the binding, and Wails has a rebind path (`requestRebind`). Show "Your desktop manages this shortcut" and use that path. Exact behavior needs a real desktop (phase 12).
- A read-only list of all in-app shortcuts, from the same table as `shortcuts.ts`, so that docs and behavior cannot drift.
- When unavailable: "Use ⌘K in the main window or the tray instead", as the banner says today.

## 10.5 General

- App version, and links to docs and the log file.
- Keep tunnels running when the window closes: not a setting. It is decided by tray availability (product.md). Explain the current behavior as text.
- Update status (phase 13 fills this in).
- Launch at login: not in product.md. Leave it out of v1, and note it as a likely request.
- App log: `log.Printf` goes to stderr today, which a launched app loses. Add a small rotating file in the state directory, set up in `main.go`. The log contains no secrets. Tunnel output stays in memory only, as architecture.md states.

## Tests

- `settings` round trip, with unknown keys rejected in the same way as `tunnels.toml`.
- `SetToolPath`: rejects a relative path and a non-executable file.
- `shortcuts.ts` table is rendered in the list, and the component test catches a key with no label.
- Shortcut change on macOS native: new binding works, old one no longer does, and a conflict with a system shortcut shows the error, if the OS reports it.

## Done when

- Nothing in the product requires opening `tunnels.toml` or `settings.toml` by hand.
- README's "per-tool path settings" row is done, and platform.md check 7 has a working override as its fallback.
