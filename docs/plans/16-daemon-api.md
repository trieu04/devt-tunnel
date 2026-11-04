# Phase 16: Daemon and local API

**Goal:** the engine runs in its own process, reachable over a local socket. The GUI does not use it until phase 17, so nothing changes for users yet.

**Implementation:** daemon/API and lifecycle CLI implemented. Contract, process, SSH forwarding and headless build checks cover the implementation; native Windows/Linux, signed app launch and detached Keychain access remain unverified.

**Depends on:** 14, 15. Design: [control-plane.md](../design/control-plane.md#local-api).

## 16.1 Platform additions

| Function | Unix | Windows |
|---|---|---|
| `ControlSocket() (string, error)` | `DTT_SOCKET`, then `$XDG_RUNTIME_DIR/devt-tunnel/ctl.sock` on Linux, then `<state dir>/run/ctl.sock`. Over 100 bytes: a hashed per-user directory under the OS temp directory | Same order, without the XDG step |
| `PreparePrivateDir(path) error` | Create `0700`. Refuse a symlink, another owner, or group or other access | Create. Inherit the profile ACL |
| `AcquireLock(path) (release, error)` | `flock`, non-blocking; `ErrLocked` | `LockFileEx`, fail immediately |
| `CheckPeer(net.Conn) error` | `SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS, compare the UID. Other Unix: directory permissions only | Directory ACL only |
| `Detach(*exec.Cmd)` | `Setsid` | `DETACHED_PROCESS`, `CREATE_NEW_PROCESS_GROUP` |
| `OpenRotatingLogFile(dir, name)` | Generalizes `OpenRotatingLog` | Same |

Unit tests for path selection and the over-length fallback; refusal of a shared directory; a second lock failing; a lock free after the holder exits.

## 16.2 Protocol

JSON-RPC 2.0, one message per line, in `internal/api`: wire types, method names, parameter structs and error kinds.

- `daemon.hello` must come first and carries `{apiVersion, client: {kind, name, version}}`.
- Errors use `data.kind`: `conflict`, `needs_secret`, `keyring_unavailable`, `stopped`, `unknown`, `internal`, `incompatible`, `timeout`. These are the source for the CLI exit codes and MCP error codes.
- Stored secret values enter only through `secret.set`; credential answers may enter through `prompt.answer`. Neither method returns values, and the server never logs parameters. MCP clients cannot answer prompts.

Methods mirror the engine, see the design's table. Leases, approvals and agent policy come in phase 19.

## 16.3 `internal/control` (server)

- A generic `handle[P, R]` registers each method. Each request runs in its own goroutine, so a long `tunnel.waitReady` or `tunnel.stop` blocks nothing else. Its context ends when the connection closes.
- Each connection has a mutex-guarded writer with a deadline, and a small event queue. A client that cannot keep up is disconnected and resyncs on reconnect, so the engine never blocks on it.
- `CheckPeer` runs before any read.
- Request admission holds a short lock; idle-only shutdown rejects in-flight mutations instead of waiting for them. Unconditional shutdown closes admission and stops tunnels without draining RPC handlers blocked in OS calls. Credential operations stop their engine continuations at shutdown; an already-issued native credential write may still complete in the OS.
- Config writes and reloads serialize the disk snapshot through application to the supervisor; queued operations check engine lifetime before persistence and application. Settings saves serialize copied snapshots under a separate mutex, so filesystem I/O never holds the lifecycle mutex. An already-issued filesystem write may finish after shutdown, but its engine state is not applied; unrelated start, stop and readiness calls remain concurrent.

## 16.4 `internal/client`

```go
func Dial(ctx, socket string, info api.ClientInfo) (*Client, error)
func Ensure(ctx, opts EnsureOptions) (*Client, error)
func (c *Client) Call(ctx, method string, params, result any) error
func (c *Client) Events(func(api.Event))
func (c *Client) Done() <-chan struct{}
```

`Ensure` dials and, if nothing answers, starts `dtt daemon run` with `Detach`, then polls until the context ends. `DaemonPath` looks beside the running executable, as `askpassHelperPath` does, then on `PATH`.

Clients verify the connected peer and private socket directory before sending hello or any other bytes. Unix checks ownership and permissions; Windows still relies on the inherited per-user ACL pending native acceptance.

Version handling:

| Found | Nothing active | Tunnels active |
|---|---|---|
| Same API, different build | Shut it down and start ours | Keep using it |
| Different API | Shut it down and start ours | `incompatible` error, naming `dtt daemon restart` |

## 16.5 `internal/daemon` and `cmd/dtt`

`daemon.Run`: lock, private directory, log, remove any stale socket, engine, listen, serve.

- Shutdown from `daemon.shutdown`, `SIGTERM` or `os.Interrupt` stops all tunnels first, then replies, then exits. `SIGHUP` is ignored.
- Idle exit after 5 minutes with no `Active()` and no connections. `--idle-exit D`, with `0` for never.
- Activity and daemon status use runtime state without keychain or file reads; pending prompts have a separate mutex from settings I/O. Presentation status still checks secret availability.

`cmd/dtt` has only `daemon run|status|stop|restart` and `version` in this phase, using the standard `flag` package. Phase 18 replaces that with `cobra`.

Build `dtt` in the darwin, linux and windows `build` tasks, as `devt-tunnel-askpass` is. Packaging comes in phase 21.

## Tests

- **Contract, `internal/control`:** a real socket and a real engine with `secret.Memory`. Every registered method is called at least once. A test fails when `internal/api` lists a method with no handler, or a call has no hello.
- **Process, `internal/daemon`, using a compiled `cmd/dtt` test binary with a test-only `secret.Memory` injection and a temporary `HOME` and `DTT_SOCKET`:**
  - A `command` tunnel running `sleep` survives the client disconnecting and is visible to a new client.
  - `daemon.shutdown` stops the process tree, and the daemon exits.
  - A second daemon exits while the first keeps working. Concurrent `Ensure` calls end with one daemon.
  - `kill -9` on the daemon, then a restart: a stale socket is replaced, and the child appears in `Overview.Orphans` on Unix.
  - Idle exit fires with a short `--idle-exit`, and does not fire while a tunnel runs.
  - A real SSH tunnel with the private `sshd` from `internal/e2e`, through the daemon, including a passphrase from `secret.Memory` and an answered prompt.
- **Boundary:** `go list -deps ./cmd/dtt` includes no `wailsapp`. `CGO_ENABLED=0 GOOS=linux go build ./cmd/dtt` and the same for windows and darwin pass in CI.
- **Regressions:** rejected peers/directories receive no hello bytes; a blocked secret store does not block runtime status, idle exit, cancellation or shutdown, including blocked starts and secret mutations; idle-only shutdown rejects in-flight mutations; staged config saves/reloads cannot apply an older snapshot after a newer commit.

## Exit test

With the app closed, a script using `internal/client` starts a tunnel through a daemon it launched, and exits. The tunnel keeps forwarding, and `dtt daemon status` shows it. `dtt daemon stop` ends both.

## macOS open-check results (2026-11-04)

A disposable native `.app` opened through the Finder UI inherited the graphical login session's `SSH_AUTH_SOCK` (`/var/run/com.apple.launchd.…/Listeners`); its detached child retained it. A PTY-launched probe verified `setsid`, PPID 1 and loss of the controlling terminal. A real localhost SSH session with private sshd, keys and an empty agent showed that the detached child kept its forwarded-agent path after disconnect while that session socket became absent. `open` invoked from a shell inherited that shell's override, so it was not used as Finder evidence. No user socket was connected to or changed.

Keychain reads remain **unverified**, not failed: `go-keyring v0.2.8` cannot select a disposable keychain without changing the user's default/search list. No real keychain entry or search list was touched. Follow-up needs an isolated-keychain seam or disposable macOS user/VM; signing, hardened runtime and the real GUI-to-daemon launch also remain acceptance work. These results do not change C1–C5.

## Remaining acceptance

- Detached Keychain reads from terminal and Finder in an isolated macOS account/VM or through a supported isolated-keychain seam.
- Signed/hardened-runtime GUI-to-daemon launch and native Windows/Linux socket, ACL and lifecycle behavior.
