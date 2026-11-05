# Phase 17: GUI as a client

**Goal:** the desktop app holds no tunnel state. Every `host.Service` method reaches the daemon over the API, with no visible change for a user who only uses the app.

**Implementation:** typed forwarding, connection cache/backoff, event resubscription, lifecycle forwarding and the host-to-engine boundary are implemented with automated tests; native checks 1–9 passed on macOS, while check 10 remains pending after fixing its reveal authentication policy.

**Automated evidence (2026-11-05, macOS arm64):** host race tests, full internal/command Go tests including daemon e2e, frontend tests/typecheck/build, binding regeneration without contract changes, dependency audits and the Wails native shell build passed. The app was not launched or inspected, so none of the native-window rows below are accepted.

**Native evidence (2026-11-05, macOS arm64):** a person reported checks 1–9 passed. Check 10, covering Vault, missing-secret retry and reveal, stopped at reveal with `RuntimeError: device owner authentication is not configured`; the implementation incorrectly requested biometric-only policy value 1 instead of `LAPolicyDeviceOwnerAuthentication` value 2. The code is corrected, but check 10 is not accepted until the rebuilt app shows the OS authentication prompt and reveals the value.

**Depends on:** 16. This is the riskiest phase: `go test` cannot show that the tray, dialogs and quit still work.

## 17.1 Connection

`NewService` makes one `client.Ensure` attempt of up to 5 seconds, then a goroutine keeps the connection: wait on `Done()`, retry with backoff, and notify the UI.

While disconnected, `Overview` returns the last data with `ConfigError` set to a message such as `Cannot reach the tunnel service: ...`, shown by the existing banner, and methods return that error. The app must not crash or block its window.

Every RPC result and event callback is fenced to the client generation that produced it, so a late old-daemon response cannot replace state established by a reconnect.

## 17.2 Forwarding

Replace the engine calls from phase 14 with typed calls through a helper:

```go
func call[R any](s *Service, method string, params any) (R, error)
```

Bound signatures do not change, apart from the types moved in phase 14. Methods with no error result log failures.

| GUI concern | After this phase |
|---|---|
| `tunnels:changed` | Emitted on the `tunnels.changed` event |
| `askpass:prompt` | Emitted on `prompt.added`, which also shows the main window. On connect, `Overview.Prompts` covers any already queued |
| Tray popup (`TrayPopup.tsx`) | Unchanged: it uses `Overview` and `Service` methods |
| `WatchLogs` | Polls `logs.since`; a sequence or daemon-generation reset emits a replace event so the frontend discards prior-generation lines |
| `StartAndCopy` | `tunnel.start`, `tunnel.waitReady`, then the clipboard locally |
| Wake | The Wails event calls `system.wake`. The daemon also has the clock-jump fallback |
| Window focus | Calls `system.focus` |
| `RevealSecret` | OS auth, then `platform.Keychain{}.Get` directly. The API returns no values |
| Settings | Read at launch through `settings.get`; writes use `settings.setShortcut` and `settings.setUpdateChecks`, so the daemon is the only writer |

## 17.3 Launch and quit

- **`StartOnLaunch`:** the GUI calls the existing phase 16 method `tunnel.startOnLaunch` once, on `ApplicationStarted`, and keeps that one request pending across an initial connection failure. The daemon never does this by itself, so `dtt ls` cannot start tunnels.
- **⌘Q and "Stop all and quit":** `OnShutdown` calls `daemon.shutdown` with the PID from its handshake and waits up to 15 seconds. A transport-only retry keeps that PID fence, so it cannot stop a replacement daemon. This keeps the product rule ([decision C3](../design/control-plane.md#decisions)).
- **Window close without a tray:** quits as above, so tunnels do not run with no way back in.
- **A GUI crash:** the daemon and tunnels keep running, and the next launch reconnects and shows them.
- **`SingleInstance` stays** for the GUI.

## 17.4 Remove in-process state

`host.Service` no longer imports `internal/engine`, `supervisor` (apart from types) or `askpass`. A test on `go list -deps ./internal/host` enforces the first of these.

## Tests

Automated:

- `host` forwarding against an in-memory fake server: events reach subscribers, a dropped connection sets the banner, and a reconnect clears it.
- Frontend tests still pass.

**Native window on macOS, by a person, recorded as evidence.** Server mode does not count:

| Check | Expect |
|---|---|
| First launch | The daemon starts, and the window shows tunnels |
| Start, stop, restart, logs, editor, profiles, Vault | As before |
| SSH host-key prompt, with the window hidden | The window comes forward and the answer works |
| Missing secret, added in the Vault | The start retries |
| Reveal | OS authentication appears, then the value |
| Tray popup and global shortcut | As before |
| ⌘Q with tunnels running | Processes are gone, and the daemon exits |
| `kill -9` on the GUI | Tunnels survive, and a relaunch shows them |
| `kill -9` on the daemon | The banner appears, then reconnects, and orphans are offered on the next launch |
| Sleep and wake | Reconnecting tunnels retry promptly |

## Exit test

Every row above passes on macOS. Add a Windows and a Linux column to the [validation matrix](12-cross-platform.md).
