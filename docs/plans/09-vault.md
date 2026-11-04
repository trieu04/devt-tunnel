# Phase 9: Vault

**Goal:** manage secrets from a screen, reveal them only after OS authentication, and retry a start once its secret is added.

**Depends on:** phases 1 (`secret.Cached`), 2, 5. **Mockup:** board "Vault". **Decisions:** D3.

## 9.1 Secret index

The mockup lists kind, users and last use, including secrets no tunnel uses (`ngrok-team`, "Not used"). `go-keyring` has only `Set`, `Get`, `Delete` and `DeleteAll`, with no way to list. So the app must keep its own index.

New `internal/core/secret/index.go`:

```go
type Kind string // ssh_passphrase, ngrok, cloudflared, env

type Entry struct {
    Name     string    `json:"name"`
    Kind     Kind      `json:"kind"`
    Created  time.Time `json:"created"`
    LastUsed time.Time `json:"lastUsed,omitempty"`
}
type Index struct{ /* path, mutex */ } // vault.json in the state directory
```

- **Names and metadata only, never values or parts of them** (D3). A test decodes the file after each operation and checks that a sentinel value is absent.
- Stored per machine in the state directory, because the keychain is too.
- `LastUsed` is written when a value is injected at spawn (`Manager.environment`) and when the askpass broker answers from the store. Both need a callback, so wrap this in the `Store` passed to them, rather than adding a dependency: an `Index`-aware `Used` hook in `secret.Cached`, called from `Get`. Reveal calls the inner store directly and does not count as use.

Listing merges three sources:

```go
type SecretView struct {
    Name     string   `json:"name"`
    Kind     Kind     `json:"kind"`
    State    string   `json:"state"`  // present, missing, unavailable
    UsedBy   []string `json:"usedBy"` // tunnel names
    LastUsed string   `json:"lastUsed"`
}
```

Names referenced in `tunnels.toml` appear even when absent from the index, for example from a shared file. Kind is inferred from the reference when the index has none. Index entries that nothing references show "Not used".

## 9.2 Service

```go
func (s *Service) ListSecrets() []SecretView
func (s *Service) SetSecret(name, value string, kind secret.Kind) error  // now takes a kind
func (s *Service) DeleteSecret(name string) error
func (s *Service) RenameSecret(from, to string) error
func (s *Service) RevealSecret(name string) (string, error)
func (s *Service) UserAuthAvailable() bool
```

- `SetSecret` enforces a name pattern, `[A-Za-z0-9._-]+`, and a size check before reaching the keychain. Credential Manager allows 2560 bytes and go-keyring reports `ErrSetDataTooBig`, so map that to a clear message.
- `DeleteSecret` returns which tunnels use it. The UI confirms before calling, then those tunnels show `Needs token` through `Statuses` (phase 1). The keychain is the source of truth for presence, so a delete that fails leaves the index as is.
- `RenameSecret`: set new, update every reference through one `Store.Update`, then delete old. If the config update fails, delete the new entry. Order matters so that no step loses the value.
- Values never reach logs. Add a test that runs the service operations with a capturing logger.
- The `Overview` gets `KeychainAvailable bool` and `KeychainName string`, for "Stored in macOS Keychain".

## 9.3 Start retries after adding a secret

product.md: "add it in the Vault and the start retries". Not implemented; a failed start is simply forgotten.

- `Manager` tracks `pendingStart map[string]bool`, set when `Start` finds a missing secret. It is cleared on `Stop`, on a successful start, or when a tunnel is removed.
- New `Manager.SecretsChanged()`: called by the service after `SetSecret`. It starts each pending tunnel whose secrets are now all present.
- Only a start the user asked for in this session is pending. A `needs_token` tunnel that nobody tried to start does not start itself when a secret is added.
- Test: start → pending → set secret → running without another call; stop while pending → set secret → stays stopped.
- From the main window, the action opens the Vault with that secret focused, and returns to the list after saving.

## 9.4 OS authentication

Add to `internal/platform`, as in platform.md:

```go
type UserAuth interface {
    Available() bool
    Confirm(ctx context.Context, reason string) error
}
```

| OS | Implementation | Notes |
|---|---|---|
| macOS | `LAContext` with `LAPolicyDeviceOwnerAuthentication` (Touch ID or password) | Needs Objective-C. See below |
| Windows | `CredUIPromptForWindowsCredentialsW`, then verify with `LogonUserW`; or Windows Hello through WinRT `UserConsentVerifier` | Start with CredUI, since it needs only `syscall` |
| Linux | `Available()` returns false | Values can be replaced but not revealed. polkit may come later |

**Build constraint:** CI runs `internal/platform` tests with `CGO_ENABLED=0`. So place the Objective-C in `userauth_darwin.go` behind `//go:build darwin && cgo`, with `userauth_other.go` returning `Available() == false` otherwise. An alternative is `purego`, which Wails already depends on, and it would avoid the split. Try that first, as it keeps one code path.

Authenticate per reveal, not per session. A cancelled prompt returns an error the UI shows quietly.

Check after signing (phase 13) that a keychain item written by an unsigned dev build remains readable by a signed build, and document the result. On macOS, go-keyring goes through `/usr/bin/security`, which may affect which app the OS prompt names.

## 9.5 Vault screen

Lives in Settings (phase 10 adds other pages): `screens/settings/VaultPage.tsx`.

- Header: "Secrets", "API keys, tunnel tokens and SSH key passphrases. Injected as env vars at start, never written to disk.", `Stored in macOS Keychain`, `+ Add secret`.
- Table: Name, Kind, Value, Used by (links to the tunnels), Last used, Actions.
- Value: fixed mask or `Missing`. Actions: `Paste token` for missing; `Reveal` and `Replace` for present. The mask has no partial value (D3).
- Reveal: after authentication, show the value with Copy and Hide. Auto-hide after 30 s, and on window blur. The value lives only in that component's state, never in `Overview`.
- Without `UserAuth`: no Reveal button. The footer says why, instead of "Reveal asks for Touch ID or the system password."
- Replace and Add: password input, no autofill, cleared after submit. Kind select for Add.
- Delete is in a row menu, confirming with the tunnel names.
- `keyring_unavailable`: full-page message explaining that a Secret Service is needed, naming common packages on Linux. Tunnels without secrets keep working. Never offer a plaintext fallback (architecture.md).

## Tests

- Index: merge rules, unreferenced entries, missing references.
- `secret.Memory`-based service tests: delete, rename rollback, retry.
- Real keychain round trip on macOS, behind an env flag so that CI does not touch it: closes platform.md check 8 for macOS.
- Component: reveal is hidden on blur; no Reveal without `UserAuth`.

## Done when

- Every row and action on the Vault board works, with the D3 change.
- A missing-secret start completes after adding the secret, with no further click.
- No test finds a secret value in any file written by the app.
