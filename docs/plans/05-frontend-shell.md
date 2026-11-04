# Phase 5: Frontend shell and main window parity

**Goal:** structure the UI to hold the coming screens, align it with the mockups, and finish the main window.

**Depends on:** phases 2 and 3 (host list and tool info for the empty search and status bar). **Mockups:** open `docs/devTTunel UI.html` in a browser; boards "Main" and "Luồng hoạt động". The mockups use the name `devTTunel` and the path `~/.config/devttunel/`; the code's `devt-tunnel` is correct.

## 5.1 File layout

`MainWindow.tsx` holds everything today. Target:

```text
frontend/src/
  api.ts                  only module talking to Go (unchanged rule)
  useOverview.ts
  app/App.tsx             screen state and global shortcuts
  app/shortcuts.ts        pure: key event → action, per OS
  screens/TunnelsScreen.tsx
  screens/EditorScreen.tsx     phase 6
  screens/ProfilesScreen.tsx   phase 8
  screens/SettingsScreen.tsx   phases 9, 10
  components/             TunnelRow, DetailPanel, TopBar, Sidebar, StatusBar, Banner, ...
  quick/                  phase 7
  lib/filter.ts           moved from src/filter.ts, with its test
```

Navigation is a `useReducer` over `{ screen, params }`, with no router dependency. There are few screens, and deep links are not needed. Moving a file is one commit, without behavior changes, so that review stays easy.

## 5.2 Design tokens

- Open the mockup boards, and move their colors, spacing, radii and type sizes into CSS custom properties at the top of `styles.css`. Then replace literal values throughout.
- Dark mode via `prefers-color-scheme`, because a tray tool runs next to whatever the OS uses. Set `BackgroundColour` for both windows to match, to avoid a white flash.
- Fonts: `frontend/public` has Inter Medium only. Add Regular and SemiBold, and JetBrains Mono with its license, since the mockups use both. Do not load fonts from a CDN, because the app must work offline.
- Keep effects simple, per the WebKitGTK risk in platform.md: no `backdrop-filter`.

## 5.3 UI tests

- Add `@testing-library/react`, `jsdom` and `@testing-library/user-event`, with a Vitest config using `jsdom`.
- `api.ts` is the single seam, so tests mock it with `vi.mock("./api")`. Add a small fixture builder for `Overview`.
- Keep logic in pure modules (`lib/`) where possible, and test components for behavior only: keyboard, disabled states, errors shown.
- Native rendering is not covered by this. Keep using server-mode for browser checks and the native window for acceptance, and say which was used.

## 5.4 Main window parity

Compared with the "Main" mockup and product.md:

| Item | Change |
|---|---|
| New tunnel button | Opens the editor; replaces "Edit tunnels.toml", which moves to Settings (phase 10) |
| Sidebar | Adds Vault & keys and Profiles links below Type |
| Heading | "All tunnels" with a summary such as "3 running · 2 favorites" |
| Tunnel row | Status text with uptime; profile chips; flag chips; type-specific subtitle for non-SSH |
| Nested forwards | Keep `ForwardLine`, listen → target |
| Row error | Inline error text with a recovery action: "Add token in Vault" for `needs_token`, Edit for failures from a bad definition |
| Empty search | "No tunnel matches “x”", with "Create tunnel for “x”". If x matches a host from `SSHHosts`, offer "Create tunnel from host" (product.md) |
| Empty list | Removes the "editor is not built yet" text. Offers New tunnel, From SSH host, Paste command |
| Detail panel | Command with Copy; key-values: PID, uptime, profiles, resolved tool path; log; Start/Stop, Edit, Duplicate actions |
| Stale | Badge "Restart to apply changes", with a Restart action, from `Status.Stale` (phase 2) |
| Status bar | Running count · `OpenSSH · ~/.ssh/config · 12 hosts` · `Secrets in OS keychain` · shortcut hints |

Uptime: compute from `status.startedAt` in a shared one-second ticker, not per row.

Fix also: `selected` falls back to `visible[0]` when the selection is filtered out, but `selectedId` then keeps a hidden id. Clear it when no longer visible.

## 5.5 Secret form in the detail panel

The inline form handles only the first secret (`secretName`), and uses `SetSecret` directly. Replace it with a "Add token in Vault" action that opens the Vault at that secret. Until phase 9 lands, keep the form but list every missing secret, using a new `TunnelView.MissingSecrets []string` computed in Go. `t.Secrets()` already lists them.

## 5.6 Global app shortcuts

In `app/shortcuts.ts`, with a table test, mapping ⌘ to Ctrl off macOS:

| Key | Action | Phase providing the target |
|---|---|---|
| ⌘N | New tunnel | 6 |
| ⌘E | Edit selected | 6 |
| ⌘⇧V | Editor in paste mode | 6 |
| ⌘K | Quick launch palette | 7 |
| ⌘↵ | Start or stop selected | here |
| ↑↓ | Move selection in the list | here |
| ⌘Q | Stop all and quit | here |

Handlers ignore plain keys while an input is focused. The WebView swallows ⌘Q on some platforms, so add a native application menu in `internal/host/desktop.go` with Quit, Edit (needed for copy and paste on macOS) and Window. Check on macOS that ⌘C and ⌘V work in inputs, as a frameless or menu-less app often loses them.

## 5.7 Quit confirmation

Tray "Stop all and quit" says what it does, but ⌘Q does not. If any tunnel is active, ask once via a Wails dialog: "Stop 3 tunnels and quit?". No prompt when nothing runs.

## 5.8 Accessibility

Keep what exists: labels, `aria-pressed`, `role="alert"`. Add focus-visible rings, keyboard reachability for every action, and contrast checked against the new tokens in both themes. Use a real list role for rows.

## Done when

- `MainWindow.tsx` no longer exists, and the app behaves as before after 5.1.
- Main window matches the board in light and dark, in the native macOS window.
- Component tests cover empty states, row actions and shortcuts.
