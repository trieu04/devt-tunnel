# Phase 1: Core fixes

**Goal:** fix defects found by reading the current code, before more is built on it. Each task starts with a failing test.

**Depends on:** nothing. **Touches:** `internal/core/*`, `internal/platform`.

## 1.1 `Needs token` never clears

`Manager.Start` stores `StateNeedsToken` in the entry (`supervisor.go:235`). `Statuses` only recomputes a tunnel whose stored state is `StateStopped` (`supervisor.go:182`). So after the user saves the missing secret, the badge and the secret form stay until the next start.

- In `Start`, do not store the secret state. Leave the entry `Stopped` and let `Statuses` derive it. Call `changed()` so the UI still refreshes.
- Keep returning nil, as `TestMissingSecretNeedsTokenAndDoesNotSpawn` expects, and check that this test still passes.
- New test: start with the secret missing → `needs_token`; `Secrets.Set` → `stopped` with no further start.

## 1.2 `~` in a profile's `ssh_config`

architecture.md's own example uses `ssh_config = "~/.ssh/config"`, and it is passed to `-F` without a shell. Confirmed with OpenSSH 10.5: `ssh -G -F '~/c' host` fails with `Can't open user config file ~/c`.

- Add `Options.Home string` to `command.Options` and an `expandHome` helper in `command`. Apply it to `SSHConfig` and `Command.Dir`. `Deps.Home` supplies the value, so the core stays free of OS calls.
- The preview shows the expanded path, because that is what runs.
- Check `-i ~/key` on a real `ssh` and record the result in a test comment. Expect no change to be needed, since ssh expands `-i` itself.

## 1.3 `Reconnecting` flips to `Starting`

`runOnce` always sets `StateStarting` after spawning (`supervisor.go:438`), but product.md defines `Running → Reconnecting → Running`. Also, the first reconnect reports `Attempt: 0`, because `attempt` is reset just before the status update in `loop`.

- Pass `retrying` to `runOnce`. Keep `StateReconnecting` with the new PID while retrying.
- Number attempts from 1 for each reconnect sequence.
- Test: fake process exits after running; assert the state sequence never contains `starting` after the first `running`, and that `Attempt` is 1 on the first retry.

## 1.4 Reconnect loop when a secret disappears

`environment` returns an error when a secret is gone, and `loop` retries forever while showing `Reconnecting`. architecture.md says that deleting a used secret puts the tunnel in `Needs token`.

- Have `environment` wrap `secret.ErrNotFound` and `secret.ErrUnavailable`. In `loop`, stop retrying on those two and end in the matching state.
- A running tunnel is not stopped when its secret is deleted. The state applies at the next spawn.
- Test both errors. Delete the secret while the fake process runs, make it exit, and assert `needs_token`.

## 1.5 Keychain read on every refresh

`Statuses` calls `Secrets.Get` for each stopped tunnel with a secret, on every notification and from each of the two windows, as well as for the tray. On macOS each `Get` starts `/usr/bin/security` (`keyring_darwin.go` in go-keyring). It also reads the value when only its presence matters.

- Add `secret.Cached`, a `Store` wrapper in `internal/core/secret`:

  ```go
  type Cached struct{ /* inner Store, presence map, mutex, clock */ }
  func (c *Cached) Has(name string) (bool, error) // cached result; Set and Delete update it
  func (c *Cached) Forget()                        // called on window focus, so outside changes show up
  ```

  Cache `ErrUnavailable` for 30 seconds only, so that an unlocked keychain is noticed. `Get` always reaches the inner store.
- `secretState` and `checks.Run` use a new helper, `secret.Has(store, name)`, which calls `Cached.Has` when the store provides it and falls back to `Get`. This keeps `secret.Store` and the existing test fakes unchanged. Spawning still uses `Get`.
- Wire it in `NewService`. Test that 100 `Statuses` calls produce one inner read.

## 1.6 Orphan stop does not confirm the stop

`Service.StopOrphan` sends SIGTERM once, without checking identity again, then removes the record and the banner even if the process ignores the signal.

- Replace `platform.KillOrphan(pid)` with `StopOrphan(pid int, identity string, grace time.Duration) error`: recheck identity (PID reuse between launch and click), terminate, wait up to `grace`, then force. Return an error if it is still alive.
- Remove the record only on success, otherwise keep the banner and show the error.
- Extend `platform_test.go` with a child that ignores SIGTERM and one whose identity no longer matches.

## 1.7 Port conflict check misses equal addresses

`checks.localPorts` compares raw strings, so `5433` and `127.0.0.1:5433` do not conflict, and cloudflared's URL is not included, although product.md lists it.

- Normalize to `host:port` using the rules in `dialAddress`. Move that helper to `command` and export it, since `supervisor` and `checks` both need it.
- `0.0.0.0` and `*` conflict with any host on the same port. Extract host and port from `Cloudflared.URL`.
- Remove the misleading comment in `localPorts`. Only mark a real conflict between two listeners as a warning, and word shared targets as "also connects to".
- Table test for each form.

## 1.8 Address for named cloudflared tunnels

`lineMatcher` looks for a URL on the `Registered tunnel connection` line, which normally has none. `defaultAddress` only reads forwards, so named tunnels have no address to copy.

- `defaultAddress` returns `https://<hostname>` for cloudflared with a hostname, and `Ngrok.Addr` as a last resort for ngrok.
- Test with a fake log line.

## 1.9 Windows and POSIX quoting

`Spec.Preview` and `SplitArgs` use POSIX rules on every OS. platform.md requires separate rules, tested separately.

- Add `type Dialect int` with `POSIX` and `Windows` to `command`. Add `Options.Dialect`, set from `runtime.GOOS` in `internal/platform` and passed through `Deps`.
- `Spec.Preview` becomes `Preview(d Dialect)`. Windows follows the `CommandLineToArgvW` rules that Go's `syscall.EscapeArg` implements. A Command tunnel on Windows shows the line as written, matching `configure` in `process_windows.go`.
- `SplitArgs(s, d)`: Windows treats `\` as literal unless it precedes `"`, and has no single quotes.
- Tests for both dialects must run on every OS, since this is pure string work: spaces, embedded quotes, trailing backslash, `C:\Program Files\x`.

## 1.10 Model validation gaps

`Config.Validate` ignores several fields:

- `Profile.OnActivate` must be empty, `filter` or `start_autostart`.
- `Profile.Autostart` entries must be defined tunnels that are members of that profile.
- `Forward.Bind` and `Target` must parse. Reject a port outside 1–65535. Unix socket paths, which contain `/`, stay allowed.
- `Ngrok.Proto` must be `http`, `tcp` or `tls`.
- `Command.ReadyLog` must compile.

Messages name the tunnel and the field, as they do now. Extend `TestLoadRejectsInvalid`.

The example in architecture.md, mirrored by `sample` in `config_test.go`, has `autostart = ["prod-db"]` with no such tunnel, so the new rule rejects it. Change both to `srv1`.

## 1.11 Linux `Pdeathsig` risk

`process_linux.go` sets `Pdeathsig: SIGTERM`. From general knowledge, on Linux this signal follows the death of the OS thread that forked rather than the process, so a Go runtime thread exit could end a healthy tunnel. This has not been reproduced here.

- Write a Linux-only test: start a child from a goroutine using `runtime.LockOSThread`, end that goroutine, and see whether the child dies.
- If it does, spawn from one dedicated long-lived locked goroutine, or drop `Pdeathsig` and rely on the orphan check that already exists. Choose the simpler one.

## Done when

- All new tests pass on CI's three operating systems.
- README Status table is unchanged except for any note this phase makes false.
- `internal/e2e` still passes with real `ssh` on macOS.
