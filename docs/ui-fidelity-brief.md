# Brief: make the frontend match the design mockup

Paste this whole file into Codex as the task.

## Goal

Implement/fix the UI in `frontend/src` so it matches the mockup `docs/devTTunel UI.html` as closely as possible: layout, colors, spacing, radii, type sizes, states, light and dark. Behavior must not regress.

## 1. Read the mockup first

`docs/devTTunel UI.html` is a 3 MB bundle (gzip+base64 pages inside `<script type="__bundler/manifest">`), so don't read it directly. Unpack it:

```bash
python3 -I docs/tools/unpack-mockup.py /tmp/devttunel-mockup
```

This writes one standalone HTML file per board:

| File | Board | Maps to |
|---|---|---|
| `01-…Lu-ng-ho-t-ng.html` | Luồng hoạt động (flows) | navigation / interaction rules |
| `02-…Tunnels.html` | Tunnels (main window) | `screens/TunnelsScreen.tsx`, `parts.tsx` |
| `03-…Quick-launch.html` | Quick launch | `quick/QuickPalette.tsx`, `QuickLaunch.tsx` |
| `04-…Tray-menu.html` | Tray menu | native, only for reference |
| `05-…Edit-tunnel.html` | Edit tunnel | `screens/EditorScreen.tsx` |
| `06-…Profiles.html` | Profiles | `screens/ProfilesScreen.tsx` |
| `07-…Vault.html` | Vault | vault / keys UI |

There is no Settings board; follow `docs/plans/10-settings.md` and reuse the same tokens. Each board file is large because fonts are inlined as base64. Strip them when reading, for example `sed -E 's/base64,[A-Za-z0-9+\/=]{200,}/base64,…/g' file.html | less`. Read the `<style>` blocks for exact values and the markup for structure. Open the files in a browser (or screenshot them) to compare.

## 2. Rules from the repo docs

Follow `docs/plans/05-frontend-shell.md` (also see `06-editor`, `07-quick-launch`, `08-profiles`, `09-vault`, `10-settings`).

- Move the mockup's colors, spacing, radii and type sizes into CSS custom properties at the top of `frontend/src/styles.css`, then replace literal values.
- Dark mode via `prefers-color-scheme`.
- No `backdrop-filter` (WebKitGTK risk, see `docs/platform.md`).
- Fonts bundled locally, no CDN: Inter (Regular/Medium/SemiBold) and JetBrains Mono, with their licenses in `frontend/public`.
- The mockup says `devTTunel` and `~/.config/devttunel/`. The code's `devt-tunnel` is correct, so don't copy the mockup's name or path.
- `frontend/src/api.ts` stays the only module that talks to Go.

## 3. Constraints

- Do not edit Go code, `frontend/bindings/`, `go.mod`, build files or workflows.
- Don't commit. The working tree already has many uncommitted changes (including the new `app/`, `lib/`, `quick/`, `screens/` folders), so leave unrelated ones alone.
- Keep existing behavior, keyboard shortcuts and tests (`app/shortcuts.test.ts`, `lib/filter.test.ts`, `quick/results.test.ts`) passing.

## 4. How to work

Go screen by screen: Tunnels (sidebar, heading and summary, rows, detail panel, status bar, empty states), Editor, Profiles, Vault, Quick Launch palette, then Settings. For each one:

1. Read the board's CSS and markup.
2. Update the component and `styles.css`.
3. Run the frontend in browser mode, screenshot it and compare against the unpacked board. Iterate until the differences are small.

Check both light and dark, and a narrow window.

## 5. Verify and report

From `frontend/`, run the type-check, `vitest run` and the production build (see `package.json` scripts). Fix anything you broke.

In the final report, list:

- which screens and files changed,
- what still deviates from the mockup and why,
- what you ran (commands and results) and whether you checked in browser or in the native window.
