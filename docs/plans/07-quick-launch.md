# Phase 7: Quick launch

**Goal:** the fastest path in the product: find anything and act on it with the keyboard, from the global window or inside the main window.

**Depends on:** phases 3, 5, 6. **Mockup:** board "Quick launch (⌘K)". **Decisions:** D4, D8.

## 7.1 One component, two hosts

`quick/QuickPalette.tsx` holds the logic and list. It renders in:

- The frameless `quick` window (`?view=quick`), as today.
- An overlay in the main window, opened by ⌘K (D8). This is also the required fallback when no global shortcut registers (platform.md).

A `host` prop supplies closing and navigation. In the quick window, "open editor" calls Go to show the main window and pass the intent. Add `Service.OpenIn(screen, param string)`, which emits `nav:open` to the main window and shows it.

## 7.2 Results model

Pure module `quick/results.ts`, tested without React:

```ts
type Item =
  | { kind: "tunnel"; view: TunnelView }
  | { kind: "host"; host: SSHHost }
  | { kind: "action"; id: ActionId; label: string; hint: string };
function buildResults(input: { query; scope; tunnels; hosts; recents; selected }): { group: string; items: Item[] }[];
```

- **Groups:** Tunnels; SSH hosts without a tunnel; Actions.
- **Scopes:** All, Tunnels, SSH hosts, Actions. Tab and Shift+Tab cycle them.
- **Hosts without a tunnel:** aliases from `SSHHosts` not used by any tunnel's `SSH.Host`.
- **Ranking:** match quality first (name prefix, then word prefix, then substring, using the existing `searchText` fields), then recency from `State.LastUsed`, then definition order. With an empty query, recents first. Expose `Overview.LastUsed`.
- **Active profile:** filters tunnels, as in the main list (phase 8). Quick launch adds a final "N more in other profiles" row, because a tunnel that cannot be found is worse than a longer list.
- **Actions:** New tunnel, Create tunnel from a pasted command, Open main window, Open Vault, Copy address and Open log for the highlighted tunnel, Stop all.
- Limit each group to a sensible number. The list must stay smooth with 500 tunnels and 1000 hosts, so add a benchmark in the test.

## 7.3 Keyboard

| Key | On a tunnel | On a host | Notes |
|---|---|---|---|
| ↵ | Start or stop | New tunnel from host | |
| ⌘↵ | Start, then copy address when ready | | See 7.4 |
| ⌘C | Copy address | | Only when the input has no selection, so that normal copy still works |
| ⌘L | Open log | | Main window, detail panel |
| ⌘E | Edit | | |
| ⌘N | | New tunnel from host | Without a host: blank |
| ⌘⇧V | Paste command | | |
| ↑↓ | Move | Move | Wraps; skips group headings |
| Esc | Close | Close | Clears the query first if not empty |
| Tab | Next scope | | |

The footer shows the hints for the highlighted item, and the row shows its ↵ action, as in the mockup: "↵ Stop".

## 7.4 Start and copy

The address is only known once the tunnel is ready, and a Cloudflare quick tunnel URL comes from its log. Keep this in Go:

```go
func (s *Service) StartAndCopy(id string) error
```

It starts, subscribes for that tunnel reaching `Running` or a terminal state, writes the address with Wails `Clipboard.SetText`, then sends a tray or in-app notice. It must keep working after the palette closes, which is why it cannot be a frontend promise. A pending copy is cancelled if the user stops the tunnel. Add `Manager.WaitReady(ctx, id)`, with a test using the fake runner.

## 7.5 Result rows

Tunnel: name, state with uptime (`Running · 38m`), first forward as `-L 127.0.0.1:5432 → db.internal:5432`, or the address. Host: alias, `user@hostname`, source file, using data already fetched and without running `ssh -G` per row.

The copied value is `Status.Address` unchanged (D4).

## 7.6 Window behavior

- Hide on blur exists. Hide after an action unless that action needs input. Toggling starts the tunnel and closes.
- A prompt arriving while the palette is open: `askUser` shows the main window, which takes focus and hides the palette. Keep that. Test on real desktops.
- Remember nothing between openings except recents, as `quick:opened` already resets the query.
- Size: mockup height. Results scroll inside. `Center` on the screen with the cursor. Check multi-monitor behavior of `Center` in Wails on each platform (phase 12).
- Fix: `QuickLaunch` uses `useOverview` and refreshes on each notification, which re-sorts under the user's cursor. Hold the order until the query changes, and update only states.

## Tests

- `results.ts`: ranking, grouping, scopes, hosts already used, recents, and profile filtering.
- Component: keyboard table above, using `vi.mock` for `api.ts`.
- Go: `WaitReady` and `StartAndCopy` logic through a thin core helper.
- Manual on macOS native: ⌥Space from another app, type, ↵, tunnel starts; window closes.

## Done when

- Every row of the mockup's board works in both hosts.
- product.md's quick launch shortcuts are all implemented.
