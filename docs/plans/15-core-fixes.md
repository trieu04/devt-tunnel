# Phase 15: Core fixes for other clients

**Goal:** fix what a GUI can hide but a script or agent cannot.

**Depends on:** 14.

## 15.1 `WaitReady` reports why

Today `Manager.Start` with a missing secret returns `nil` and leaves `stopped`, and `WaitReady` then returns `errors.New("")`.

Keep `Start` unchanged, because the Vault retry flow relies on `pendingStart`. Change `WaitReady`:

| State | Result |
|---|---|
| `running` | `nil` |
| `stopped`, with a pending start | `ErrNeedsSecret` |
| `needs_token` | `ErrNeedsSecret` |
| `keyring_unavailable` | `secret.ErrUnavailable` |
| `stopped`, otherwise | `ErrStopped` |
| `failed` | The status error, never empty |

Tests with the existing fake runner cover each row, and stopping during a wait.

## 15.2 Endpoints

`Status.Address` holds one address, and `defaultAddress` skips `-R` forwards. Add to `api.TunnelView`:

```go
type Endpoint struct {
    Kind   string `json:"kind"`   // local | remote | socks | public
    Listen string `json:"listen"`
    Target string `json:"target,omitempty"`
    Public bool   `json:"public"`
}
```

Derive it in the engine from `Forwards` and `Status.Address`. `Public` is true for ngrok and cloudflared and for a bind that is not loopback. Tests for each tunnel type, for several forwards, and for IPv6 and Unix socket binds.

## 15.3 Wire the port check

`checks.Deps.Listening` is never set outside tests, so "already in use" never fires. Set it in `checkDeps`, with a short loopback dial.

A running tunnel listens on its own port, so skip the check for a tunnel that is active. Otherwise opening a running tunnel in the editor warns about itself. Use `checkDeps` in `Findings` too, which today builds a smaller `Deps`.

Tests: a free port, a port held by another process, and a tunnel's own running port.

## Exit test

Unit tests pass. In the native window, a port held by another process shows a warning in the editor, and a running tunnel shows none.
