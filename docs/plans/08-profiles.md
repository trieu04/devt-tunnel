# Phase 8: Profiles

**Goal:** implement the active profile and its behavior on activation, the Profiles screen, and tray filtering.

**Depends on:** phases 2, 5. **Mockup:** board "Profiles". **Decisions:** D6, D7.

Today the profile selector is local React state in the main window. Nothing is persisted, the tray ignores it, and `on_activate`, `stop_non_members` and `autostart` are stored but never read.

## 8.1 Core

New `internal/core/profiles`, pure:

```go
type Plan struct{ Start, Stop []string }

// Activation returns what must happen when profile p becomes active.
func Activation(cfg model.Config, p model.Profile, running map[string]bool) Plan
func Members(cfg model.Config, profileID string) []model.Tunnel
func Remove(cfg *model.Config, profileID string) // removes it and every membership
func Duplicate(cfg *model.Config, profileID string) model.Profile // copies settings and memberships
```

Rules:

- `on_activate = "start_autostart"` starts `Autostart` members. `filter` or empty starts nothing.
- `stop_non_members` stops running tunnels not in the profile, whatever `on_activate` says.
- A tunnel both in `Start` and already running is left alone.
- "All profiles" has no activation behavior.
- Activating at app launch applies filtering only. It does not rerun `on_activate`, or a restart would unexpectedly start or stop things. `start_on_launch` remains the launch setting, and it applies regardless of active profile, as today in `Attach`.

## 8.2 SSH config per profile (D7)

`optionsLocked` picks a tunnel's first profile with `ssh_config`. Change it to prefer the active profile when the tunnel is a member.

- `Manager.SetActiveProfile(id)`. Running tunnels keep their process. Those whose fingerprint changes show `Stale` (phase 2).
- Test: tunnel in two profiles with different configs; preview changes with the active profile, and `Stale` is set while running.

## 8.3 Service

```go
func (s *Service) ActivateProfile(id string) error   // persists to state, applies the plan
func (s *Service) SaveProfile(p model.Profile, members []string) (string, error)
func (s *Service) DeleteProfile(id string) error
func (s *Service) DuplicateProfile(id string) (string, error)
func (s *Service) SetProfileAutostart(id, tunnelID string, on bool) error
func (s *Service) StartProfile(id string) error      // "Start all"
func (s *Service) StopProfile(id string)             // "Stop all"
```

- `Overview` gains `ActiveProfile string`.
- Membership lives on the tunnel (`Tunnel.Profiles`). `SaveProfile` updates those through one `Store.Update`, so that the write is atomic.
- Removing a member also removes it from that profile's `Autostart`, or `Validate` (phase 1) rejects the file.
- If the active profile is deleted, fall back to all profiles.
- A saved active profile that no longer exists, after a hand edit, also falls back without error.
- Start all collects per-tunnel errors and reports them together. One missing secret must not stop the others.
- An activation that stops tunnels does not ask. It is the setting the user chose, and the mockup has no dialog. Show a brief notice with what changed.

## 8.4 Main window

- Top bar selector calls `ActivateProfile`, not local state. `filterTunnels` reads `overview.activeProfile`.
- Sidebar counts reflect the active profile.
- A running tunnel outside the active profile is hidden from the list by design. The heading shows "2 running in other profiles", which switches to All profiles on click, so that no running tunnel is invisible everywhere.

## 8.5 Profiles screen

`screens/ProfilesScreen.tsx`, with two panes.

**Left:** list with name, meta ("4 tunnels · 1 running"), Active badge, and New profile. Below it: "A tunnel can belong to several profiles. The active profile filters the tunnel list and the tray; “All profiles” shows everything."

**Right, for the selected profile:**

- Header: name, Activate, Start all, Stop all, Duplicate.
- Fields: Name, SSH config file (with file picker, and `~` allowed), Description. Under the SSH config field: "This replaces ~/.ssh/config for tunnels in this profile" since `-F` does not add to it (architecture.md). Validate that the file exists, as a warning.
- When this profile becomes active: Only filter the list / Also start its auto-start tunnels. Checkbox: Stop running tunnels that are not in this profile.
- Tunnels: every tunnel with a membership checkbox, type, "also in Home", and an Auto-start switch enabled for members.
- Delete profile: "Tunnels stay. They are only removed from this profile." with a confirmation naming it.

Changes save explicitly, with Save and an Unsaved badge, in line with the editor. Only Activate, Start all and Stop all act immediately.

## 8.6 Tray

- `trayTunnels` filters favorites by active profile. Active tunnels always stay listed, wherever they belong, or a running tunnel could not be stopped from the tray.
- Add a "Profile" submenu with radio items, including All profiles. `AddRadio` needs checking on all platforms, and check items are the fallback.
- Add the active profile to `traySig`, so that the menu rebuilds.
- Group as Running, then Favorites, and put the running count at the top of the menu, following the mockup. "Filter tunnels" is cut (D4).
- Accelerator hints: ⌥Space and ⌘Q in labels only where the platform shows them correctly. Test on real desktops, as platform.md notes open tray issues.

## Tests

- `profiles` table tests for the rules in 8.1.
- `Store.Update` keeps membership consistent after each of save, delete and duplicate.
- Component tests for the screen's fields and autostart enabling.
- Manual on macOS native: tray updates after activating a profile from the main window, and the reverse.

## Done when

- Active profile survives a restart and filters the list, quick launch and tray.
- Each option on the Profiles board has the effect it describes.
- architecture.md's open question on several active profiles is closed.
