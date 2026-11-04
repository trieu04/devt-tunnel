# Phase 2: Persistence and the draft API

**Goal:** let the app change `tunnels.toml` safely, keep runtime state and per-machine settings, and offer the UI a way to evaluate a tunnel that has not been saved.

**Depends on:** phase 1. **Decisions:** D1, D2, D10.

## 2.1 Strict loading

`config.Load` uses `toml.Unmarshal` and ignores unknown keys. A typo such as `forwards` for `forward` silently loses the setting, and the next save from the app would delete it from the file.

- Use `toml.Decode`, and read `MetaData.Undecoded()`.
- Treat unknown keys as a load error, naming the key. The existing "last good definitions" banner already shows it.
- Test with a misspelled key at each nesting level.

## 2.2 Config store

New file `internal/core/config/store.go`:

```go
var ErrConflict = errors.New("tunnels.toml changed on disk")

type Store struct{ /* path, mutex, last-seen sha256 */ }

func NewStore(path string) *Store
func (s *Store) Load() (model.Config, error)
func (s *Store) Changed() (bool, error)                                 // file differs from last load or write
func (s *Store) Update(fn func(*model.Config) error) (model.Config, error)
```

`Update` does the following under the lock:

1. Read the file and compare its hash to the last seen. A difference returns `ErrConflict`, so the app never overwrites an outside edit.
2. Decode a fresh copy, apply `fn`, validate.
3. Copy the old file to `tunnels.toml.bak` (D1).
4. Write atomically with the existing `Save`. Record the new hash.

Saving does not change the file's layout beyond what the encoder does. `TestSaveRoundTrip` must hold for all four tunnel types with every field set.

The first save from the app replaces `configTemplate`'s comments. Add a one-line header comment to the written file: `# Written by devt-tunnel. Hand edits are kept; comments are not.`

Tests: conflict detection, `.bak` content, failed validation leaves the file untouched, concurrent `Update` calls serialize.

## 2.3 Runtime state

New package `internal/core/state`:

```go
type State struct {
    ActiveProfile string               `json:"activeProfile,omitempty"` // empty means all profiles
    LastUsed      map[string]time.Time `json:"lastUsed,omitempty"`      // tunnel id → last start
    Windows       map[string]Bounds    `json:"windows,omitempty"`
}
type Store struct{ /* path, mutex */ }
func (s *Store) Get() State
func (s *Store) Update(fn func(*State)) error
```

- Stored as `state.json` in `platform.StateDir()`, written the same atomic way as `FileRecords`. `processes.json` stays separate, since it changes on every spawn.
- A corrupt file is logged and treated as empty. It must never stop the app from starting.
- `Windows` restores the main window's size and position. Validate against current screens, so that a window cannot reopen off-screen.

## 2.4 Settings

New package `internal/core/settings`:

```go
type Settings struct {
    Tools    map[string]string `toml:"tools,omitempty"`    // tool name → absolute path
    Shortcut string            `toml:"shortcut,omitempty"` // accelerator; empty means the OS default
    Shell    string            `toml:"shell,omitempty"`    // Windows: cmd or powershell; Unix: empty means $SHELL
}
func Load(path string) (Settings, error)
func Save(path string, s Settings) error
```

- `platform.SettingsFile()` returns `settings.toml` next to `tunnels.toml` (D2).
- `platform.Tools.Overrides` is read without a lock and `PATH()` is computed once. Add a mutex and `SetOverrides`, since settings can now change at run time.

## 2.5 Model additions

- **`ExtraArgs` on `Ngrok` and `Cloudflared`.** architecture.md says unrecognized arguments are kept in `extra_args`, but only `SSH` has that field. Add it, and have `buildNgrok` and `buildCloudflared` append the split arguments.
- **`Spec.Fingerprint()`**: hash of tool, args, dir and secret names. Used by task 2.6.

## 2.6 Saving while a tunnel runs

`SetConfig` keeps running tunnels on their old settings, and nothing tells the user.

- The supervisor keeps the fingerprint of the spec a tunnel was started with. `Status` gains `Stale bool`, true when the current definition's fingerprint differs.
- Add `Manager.Restart(id string) error`: stop and wait, then start. It keeps the entry's log.
- Tests: editing a running tunnel sets `Stale`; `Restart` clears it; cosmetic changes such as the name do not set it.

## 2.7 Split the service and add the draft API

`internal/host/service.go` is 419 lines and will more than double. Split it before phases 8–10 run in parallel. Everything stays on the one `Service` struct, so generated bindings do not change.

| File | Holds |
|---|---|
| `service.go` | Struct, `NewService`, `Overview`, `notify` |
| `service_tunnels.go` | Start, Stop, Toggle, Logs, and the methods below |
| `service_prompts.go` | Askpass prompts |
| `service_orphans.go` | Orphans |

Then add to `service_tunnels.go`:

```go
type DraftResult struct {
    Preview    string           `json:"preview"`
    BuildError string           `json:"buildError"`
    Findings   []checks.Finding `json:"findings"`
}

func (s *Service) EvaluateDraft(t model.Tunnel) DraftResult
func (s *Service) NewDraft(kind model.TunnelType) model.Tunnel       // defaults per type
func (s *Service) DuplicateDraft(id string) (model.Tunnel, error)    // new id, name + " copy"
func (s *Service) SaveTunnel(t model.Tunnel, start bool) (string, error)
func (s *Service) DeleteTunnel(id string) error
func (s *Service) SetFavorite(id string, favorite bool) error
```

- `Manager.Preview` takes an id today. Add `Manager.PreviewDraft(t)`, sharing `optionsLocked`. Preview and execution must keep using the same builder.
- `SaveTunnel`: an empty id means new, so generate one (`model.NewID(name, taken)`: a slug with a numeric suffix on collision, D10). An existing id replaces in place, keeping list order. `start` starts a stopped tunnel, and restarts a running one.
- `DeleteTunnel` also removes the id from each profile's `Autostart`. `SetConfig` already stops a removed tunnel.
- `NewDraft` for SSH sets flag `-N`, `ServerAliveInterval=30` and `Reconnect: true`. Without a keepalive a dead connection can look alive for a long time.
- Return `ErrConflict` with a clear message. The UI's response is defined in phase 6.
- `loadConfig` and all writes go through `config.Store`. `NewService` builds the stores.

## 2.8 Reload on focus

Without a new dependency: on `WindowFocus` of the main window, call `Store.Changed()` and reload if true. Also call `secret.Cached.Forget()` there. The Reload button remains.

## Tests

- Unit tests per package above.
- `host` has no tests today, because it imports Wails. Keep `service_tunnels.go` logic thin by moving id generation and profile cleanup into `model`, where they can be tested with `CGO_ENABLED=0`.

## Done when

- A round trip through `SaveTunnel` and reload keeps every field for all four types.
- Editing the file by hand while a draft is open gives a conflict rather than a lost edit.
- Active profile and window size survive a restart.
