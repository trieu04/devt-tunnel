# Phase 14: Engine extraction

**Goal:** everything in `host.Service` that is not Wails moves to a package the daemon can use. The app behaves exactly as before, still in one process.

**Implementation:** extracted in-process; automated checks cover lifecycle, prompts, events and the headless boundary. Native macOS window start/stop/prompt acceptance remains unverified.

**Depends on:** nothing. Design: [control-plane.md](../design/control-plane.md).

## 14.1 Shared types

Create `internal/api` for types every client shares: `TunnelView`, `PendingPrompt`, `Overview`, `DraftResult`, `LogAppend`, `SSHHostList`, `ToolInfo`, `SecretView`. `UpdateInfo` stays in `host`, since update checks stay in the GUI.

`Overview.Shortcut` and `ShortcutError` stay in the type, so the frontend is unaffected. The GUI fills them in, and the engine leaves them empty.

Regenerate bindings with `wails3 task common:generate:bindings`. Update the type imports in `frontend/src/api.ts` and `frontend/src/PromptDialog.tsx`. Check for others with `grep -rn 'internal/host"' frontend/src`.

## 14.2 `internal/engine`

Move `service_tunnels.go`, `service_profiles.go`, `service_ssh.go` and `service_vault.go`, plus the wiring in `NewService`, and rename the receiver. Settings persistence also moves to the engine so shortcut, tool and update preferences have one writer; the host keeps update HTTP requests and shortcut registration.

```go
type Options struct {
    Version       string
    ConfigPath    string       // zero values use internal/platform
    SettingsPath  string
    StateDir      string
    Secrets       secret.Store
    Runner        supervisor.Runner
    AskpassHelper string
}

func New(Options) (*Engine, error)
func (e *Engine) Subscribe(func(api.Event)) (cancel func())
func (e *Engine) Shutdown()
func (e *Engine) Active() bool // any tunnel starting, running or reconnecting, or a prompt pending
```

Stays in `host`, because each needs a window, the clipboard or OS auth:

| Method | Reason |
|---|---|
| `ShowMain`, `ShowQuickLaunch`, `HideQuickLaunch`, `OpenIn`, `Quit` | Windows |
| `StartAndCopy` | The clipboard. Calls engine `Start` and `WaitReady` |
| `WatchLogs` | Wails events. Polls engine `LogsSince` |
| `RevealConfig` | File manager. The engine provides `EnsureConfigFile() (string, error)` |
| `RevealSecret`, `UserAuthAvailable` | OS auth, then a direct keychain read |
| `SetShortcut` | Global shortcut registration. Persisting goes through the engine |
| `CheckForUpdate`, `AppVersion` | GUI feature |

## 14.3 Events instead of Wails calls

- `notify` emits a coalesced `tunnels.changed` to subscribers, in place of `Event.Emit`.
- `askUser` queues the prompt and emits `prompt.added`. The host's subscriber emits the Wails event and shows the main window.
- Move `monitorClockJump` and `clockJumped` to the engine, with `Wake()` public for the GUI's OS event. Shutdown cancels the monitor, pending prompts, notifications and broker.
- `Focus()` wraps the `secrets.Forget` and `configStore.Changed` logic from the window focus hook.

## 14.4 Host forwards in-process

`host.Service` keeps every bound signature and forwards to `*engine.Engine`. Its methods change only in body. `desktop.go` calls the engine for `StartOnLaunch`, wake and focus.

## 14.5 Guard the boundary

- A test or CI step fails if `go list -deps ./internal/engine ./internal/api` includes `wailsapp`.
- Add `./internal/api/... ./internal/engine/...` to the package lists in `ci.yml` and `release.yml`, which run with `CGO_ENABLED=0`.

## Tests

- Move `TestNewDraftUsesConsistentLocalServiceURL` to the engine.
- Engine tests with `secret.Memory` and temporary paths: save, start, stop, delete, conflict on outside edit, prompt queue and answer, `Active`.
- Existing `internal/e2e` still passes.

## Exit test

`go test ./internal/... ./cmd/...` passes, `pnpm --dir frontend test` passes, and one tunnel starts, stops and answers an SSH prompt in the native macOS window with no visible change.
