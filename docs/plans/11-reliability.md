# Phase 11: Reliability

**Goal:** behave correctly through sleep, network changes and real tool output, and stop polling logs.

**Depends on:** phase 1. Can run in parallel with phases 2–10.

## 11.1 Wake from sleep

Required by platform.md; nothing handles it yet. Wails 3.1.0 defines `events.Common.SystemWillSleep` and `SystemDidWake` (`pkg/events/events.go`), and Windows has `APMResumeAutomatic` and `APMResumeSuspend`. Which of these fire on each OS has not been checked, so verify in phase 12 and fall back to 11.2 if one does not.

```go
func (m *Manager) Wake()
```

- For a tunnel in backoff: end the wait now, and reset backoff to base. Implement with a per-entry `wake chan struct{}` selected alongside `time.After` in `loop`.
- For a running tunnel with `Ready.Ports`: recheck that the ports accept connections. A listening port does not prove that the SSH connection is alive, because `ssh` keeps its listener until `ServerAliveInterval` gives up. So this catches a dead process, and not a dead connection.
- For a running tunnel with `Reconnect` on: treat it as stale only after a grace period (keepalive default 30 s × count), and let ssh's own exit drive the reconnect. Do not kill a healthy tunnel on every wake.

Tests with the fake runner: wake during backoff restarts immediately; wake with a healthy tunnel does nothing; backoff is reset after wake.

## 11.2 Clock jump detection

As a fallback for platforms where the wake event does not fire: a goroutine in the service ticks every 30 s, comparing wall-clock time and calling `Wake` on a jump over 60 s. Go's monotonic clock pauses in sleep on some platforms, so compare `Round(0)` values. Put the check in a small pure function, tested with a fake clock.

## 11.3 Network change

A long `ServerAliveInterval` means slow recovery after a Wi-Fi change, but it is ssh's job to notice, and the supervisor reconnects when it exits. So no listener in v1. The `NewDraft` keepalive default from phase 2 covers the common case. Document this in architecture.md. Tunnels with no keepalive get a warning in checks.

## 11.4 Real tool output

Readiness patterns (`cloudflaredQuickReady`, `cloudflaredNamedReady`, `ngrokReady`) come from general knowledge and fake logs only. The real tools have never been run in tests.

- Capture real logs from current `cloudflared` and `ngrok` releases into `internal/core/supervisor/testdata`, with tokens and hostnames removed. Assert each pattern and address extraction against them. **Needs accounts**, see the plans README.
- Confirm flags on current releases: `ngrok --domain` versus `--url`, and whether `--domain` is valid with `tcp`; `cloudflared tunnel --url … run` ordering.
- `internal/e2e` gets optional tests behind `DEVT_TUNNEL_E2E_CLOUDFLARED=1`, for a quick tunnel, which needs no account.
- Record the tested versions. architecture.md notes dependence on installed versions, so a check may warn on a very old version.
- ngrok's free-tier limit of one session is a common failure. Show its error line from the log as `Status.Error`, rather than "process exited".

## 11.5 Better failure messages

`Status.Error` is usually `exit status 255`. Keep the last meaningful line of output:

- `logRing` remembers the last non-empty output line before exit.
- `loop` uses it in the message, for example `ssh: connect to host … Connection refused`.
- Test that note lines the supervisor writes itself are not used.
- Output could contain sensitive values for Command tunnels, but it is already shown in the log panel, so this adds no exposure.

## 11.6 Streaming logs

`Detail` polls `Service.Logs` every second, copying up to 2000 lines each time.

- Add a `logs:appended` event carrying tunnel id and new lines, coalesced to 100 ms. Emit only for the tunnel a window subscribes to, through `Service.WatchLogs(id)`.
- `DetailPanel` keeps a buffer with a line cap, following the tail unless the user has scrolled up. Clear and Copy actions.
- `Service.Logs` remains for the initial fill. Use a `seq` number so that no line is lost or duplicated at the join. Test this at the `logRing` level.
- `lineMatcher` runs per line on the writer's goroutine, so check that a slow subscriber cannot block a child's output: the event goes through a bounded buffer that drops the oldest lines and adds a "… N lines skipped" note.

## 11.7 Prompts after the window is gone

`askUser` waits up to the broker's 2-minute timeout. If several tunnels ask at once, `PromptDialog` shows them one by one, in map order (`Overview` iterates a map), so that order can jump. Sort by prompt sequence number. Also add a visible "Waiting for your answer" state on the tunnel's row, and a tray indicator, so that a hidden prompt is not mistaken for a hang. Show a countdown near the timeout.

When a prompt is cancelled, `ssh` may retry the prompt several times. Mark the tunnel as cancelled, so that a Cancel ends the attempt rather than asking again, and test with the private `sshd` in `internal/e2e`.

## 11.8 OTP and keyboard-interactive

platform.md check 6 has "OTP not run". Add an `internal/e2e` test where `sshd` uses keyboard-interactive auth through a PAM-free setup, if that is possible without root. Otherwise record it as a manual check. Classification currently treats unknown text as `KindOther` with hidden input, which suits OTP.

## Done when

- Close the lid for ten minutes on a Mac with one forward running: the tunnel returns to Running within about one backoff after wake, with no manual action.
- Each pattern is tested against real captured logs.
- No polling in the frontend; CPU is flat with a tunnel selected and idle.
