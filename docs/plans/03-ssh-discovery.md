# Phase 3: SSH discovery, tool info and complete checks

**Goal:** list hosts from `~/.ssh/config`, show their resolved values, report which binaries were found, and finish the editor checks that product.md lists.

**Depends on:** phase 1.

## Correction to architecture.md

architecture.md says the host list comes from `ssh -G`. That cannot work alone:

- `ssh -G` takes one alias and has no way to enumerate.
- It also succeeds for hosts that do not exist. Confirmed with OpenSSH 10.5: `ssh -G nosuchalias-xyz` prints a config and exits 0. So it cannot implement the "host exists" check either.

So: a small parser finds names only, and `ssh -G` stays the only source of resolved values. Update architecture.md in this phase.

## 3.1 Host name scanner

New package `internal/core/sshconfig`:

```go
type Host struct {
    Alias  string `json:"alias"`
    Source string `json:"source"` // file that defines it
}
func Scan(path string, fsys FS) ([]Host, []string, error) // hosts, warnings
```

It handles only what is needed to find names:

- `Host` lines with several patterns, keyword case-insensitive, with `=` or spaces and quoted values.
- Skip patterns with `*` or `?` and negated `!` patterns.
- `Include`: glob expansion, `~`, and relative paths resolved against `~/.ssh`, as ssh does. Includes inside a `Host` block still apply. Guard against cycles, with a depth limit of 16 as OpenSSH uses.
- `Match` blocks define no aliases, so ignore them.
- De-duplicate by alias, first definition wins. Sort by file order.

It must not interpret any value other than these. `FS` is an interface so that tests use in-memory files, keeping the package free of OS code.

Table tests cover each rule above, including a missing Include file (warning, not error) and CRLF files.

## 3.2 Resolved values

Same package:

```go
type Resolved struct {
    HostName, User, Port, ProxyJump string
    IdentityFiles                   []string
}
func ParseG(output []byte) Resolved
```

The caller runs the command, keeping this pure. `internal/platform` adds `RunTool(ctx, path string, args ...string) ([]byte, error)`, using a clean environment and the hidden-window setup from `configure`. Timeout 3 s.

`ssh -G` can run `Match exec` commands from the user's config, as any `ssh` invocation would. Run it only for an alias the user selected or an explicit reload, and never for every host at startup.

## 3.3 Service methods

```go
type SSHHostList struct {
    ConfigPath string         `json:"configPath"`
    Hosts      []sshconfig.Host `json:"hosts"`
    Warnings   []string       `json:"warnings"`
}
func (s *Service) SSHHosts(profileID string) SSHHostList          // cached by file mtimes
func (s *Service) ResolveSSHHost(alias, profileID string) (sshconfig.Resolved, error)
func (s *Service) ReloadSSHHosts()
```

- `profileID` selects that profile's `ssh_config`, defaulting to `~/.ssh/config`.
- `-F` replaces the default config, as architecture.md notes. The editor shows which file was read (phase 6).

## 3.4 Tool info

```go
type ToolInfo struct {
    Name    string `json:"name"`
    Path    string `json:"path"`    // empty when not found
    Version string `json:"version"`
    Source  string `json:"source"`  // settings, login shell, known directory, or PATH
    Error   string `json:"error"`
}
func (s *Service) Tools() []ToolInfo
```

- `platform.Tools.Resolve` returns only a path. Add `Locate(tool) (path, source string, err error)`, keeping `Resolve` as a thin wrapper.
- The version comes from `ssh -V` (stderr), `ngrok version` and `cloudflared --version`, cached until settings change. The status bar in phase 5 uses it for `OpenSSH · ~/.ssh/config · 12 hosts`.
- A parsed OpenSSH version is also used by checks: warn below 8.4, where `SSH_ASKPASS_REQUIRE` is ignored.

## 3.5 Finish `checks`

Today `checks.Run` covers binary, `-N`, secret and port conflict.

- `Finding` gains `Field string` (`json:"field,omitempty"`), for example `ssh.host` or `forwards[1].bind`, so that the editor can place messages next to inputs.
- Add `Severity OK`: the mockup shows passed checks as well. Passed checks: binary with its resolved path, host found, ports free of conflicts, secrets present.
- `Deps` gains `Hosts func(profileID string) ([]string, error)`. New finding: alias not in the list is a **warning**, not an error, because `ssh` also accepts a bare hostname and our scanner skips wildcard patterns.
- New: `-f` in flags or extra arguments is an **error** (architecture.md: never use `-f`). `-M`, `-S` or `ControlMaster` set by the user is a warning, because it can take the forward away from the supervised process.
- New: a listening port already taken by something else is a warning at edit time, using a quick local bind test. Skip the tunnel's own port if it is running.
- `-A` is a warning, as forwarding an agent exposes it to the remote host.

`Run` takes a `model.Tunnel` already, so it works for drafts unchanged. Update `checks_test.go` for each finding. Note that `Run` returns early when `Build` fails: return that as a finding with `Field` set where possible.

## Done when

- `go test ./internal/core/...` covers the scanner against fixtures with `Include`, `Match` and wildcards.
- On a real machine, `SSHHosts` matches the aliases in `~/.ssh/config`, and `ResolveSSHHost` matches `ssh -G` by hand.
- architecture.md describes the scanner and `ssh -G` split.
