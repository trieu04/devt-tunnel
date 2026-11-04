# Phase 13: Distribution

**Goal:** a person can install, trust, run and later update the app on all three platforms, and releases come from one workflow.

**Depends on:** phase 12. Unsigned tester builds (13.2) can start earlier. **Needs from the owner:** see the plans README.

Scope from platform.md: signed notarized `.dmg` and Homebrew cask; signed NSIS installer; deb and rpm. Later: WinGet, Scoop, AppImage, AUR, in-place updates. Excluded: Mac App Store, Flatpak, strict Snap.

## 13.1 Version and identity

- One source of truth for the version: `build/config.yml`, which holds `0.1.0` and feeds Wails's assets. Stamp the Go binary with `-ldflags -X main.version=…` from the tag, in each platform Taskfile. Settings → General displays it. Add a CI step failing when the tag and config differ.
- The helper needs no version check today. Because helper and app are from one package, keep it that way.
- Add a `LICENSE` file and replace `UNLICENSED` in `nfpm.yaml`, once the owner chooses.
- Bundle identifier `io.github.trieu04.devt-tunnel` stays. It keys the single-instance lock.

## 13.2 Release workflow

New `.github/workflows/release.yml`, on tags `v*`. Native builds only, per platform.md:

| Runner | Output |
|---|---|
| `macos-latest` arm64 | `.app` zip and `.dmg` |
| macOS x64 runner | `.app` zip and `.dmg`; check availability, or build `universal` if `darwin:package:universal` proves reliable |
| `windows-latest` | NSIS installer |
| `ubuntu-24.04` | deb, rpm |

- Each job runs tests first, then packages, then uploads as a draft GitHub Release with `SHA256SUMS`.
- Signing steps run only when their secrets exist, so that forks and tester tags still produce unsigned builds, clearly named `-unsigned`.
- Pin actions by commit hash and Wails at `v3.1.0`, in line with the "pin one version" rule.
- A person publishes the draft after 13.7.

## 13.3 macOS signing and notarization

The build currently signs ad hoc (`codesign:adhoc` in `build/darwin/Taskfile.yml`).

- Sign helper first, then the app, without `--deep`: `Contents/MacOS/devt-tunnel-askpass` already is copied into the bundle. Use hardened runtime.
- Entitlements: hardened runtime and a WebView app usually need few. Add only what testing shows. Check that spawning `ssh` and `security` works under hardened runtime.
- `notarytool submit --wait`, then `stapler staple` on the `.dmg`. Use an App Store Connect API key in CI secrets.
- Test on a clean Mac that has never seen the app, with quarantine set by a real browser download.
- Re-check the Keychain prompt wording and the item's accessibility across unsigned to signed upgrades (phase 9).
- Homebrew cask: a personal tap first (`trieu04/homebrew-tap`), with the formula bumped by the workflow. Submit to homebrew-cask once the notability bar is met.

## 13.4 Windows

- Sign the app, the helper, and then the installer. The existing `sign` and `sign:installer` tasks cover the app and installer, but not the helper: `devt-tunnel-askpass.exe` is built in the Taskfile and packed by `wails_tools.nsh`, so add it. An unsigned helper launched by `ssh` is an obvious SmartScreen and antivirus risk.
- Signing option: Azure Trusted Signing or an OV certificate; the owner decides, and the cost is real. `wails3 tool sign` takes a certificate or thumbprint, so check that it supports the option chosen.
- WebView2: the installer must bootstrap it for Windows 10. Confirm `project.nsi` does this on a clean Windows 10 VM.
- Uninstall: stops a running app first, and leaves user data in `%APPDATA%\devt-tunnel`, keeping it by default. `project.nsi` runs `RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"` for the WebView2 data path, so confirm on a real uninstall that this does not reach `tunnels.toml`.
- WinGet and Scoop manifests come later.
- MSIX templates exist under `build/windows/msix`. MSIX's containerization could conflict with spawning host binaries and with `%APPDATA%` paths, so exclude it for now, and delete the template if it stays unused.

## 13.5 Linux

- `nfpm.yaml` installs to `/usr/local/bin`. Debian policy reserves that for local admins, so use `/usr/bin` for the launcher. `askpassHelperPath` looks next to the executable, so keep the two together. Preferably install to `/usr/lib/devt-tunnel/` with a symlink in `/usr/bin`, and resolve symlinks using `filepath.EvalSymlinks` in `askpassHelperPath`.
- Dependencies are correct for Ubuntu 24.04 and Debian 13. Add `openssh-client` as a recommendation. Confirm the rpm overrides on Fedora.
- Check `.desktop` generation: `nfpm.yaml` expects `build/linux/devt-tunnel.desktop`, generated at build time by `generate:dotdesktop`, while the repo has `build/linux/desktop`.
- Install and remove scripts must not fail with no desktop database present.
- AppImage later: `CleanEnviron` is ready, and platform.md warns about its cost.
- An apt or rpm repository is later. GitHub Releases is enough for v1.

## 13.6 Updates

platform.md says: start with package managers and a "new version" notice, then in-place updates with signature checks.

**v1: notice only.**

```go
func (s *Service) CheckForUpdate() (UpdateInfo, error)
```

- Calls the GitHub Releases API for the latest non-draft tag, at most once a day and when the user asks, and shows it in Settings → General and as a dismissible main window banner.
- A setting to turn off the check. It is the app's only network request of its own, so state that in the docs.
- Compare semantic versions, and ignore prereleases unless running one.

**After v1: in-place updates.** Wails 3.1.0 has `pkg/updater`, with providers for GitHub, appcast, endpoint and keygen, and it handles verification, atomic swap and restart. platform.md marks it as not evaluated, so run a separate evaluation first. Questions to answer: does it replace the helper as well as the main binary; how does it treat tunnels running during an update, which must stop and restart with the app; and how does it fit Homebrew and deb installs, where the package manager owns the files.

## 13.7 Release checklist

Written to `docs/release.md`:

1. All CI green on the tagged commit.
2. For each OS, install on a clean machine from the draft artifacts.
3. Run: create an SSH tunnel from a config host, start, connect through it, quit. Confirm that no process is left.
4. Upgrade from the previous version: `tunnels.toml`, settings, state and secrets survive.
5. Uninstall leaves user data as documented.
6. Record installer sizes and idle memory, which closes platform.md check 10.
7. Publish; bump the Homebrew tap.

## 13.8 User docs

- README: install section per OS, prerequisites (the app does not bundle `ssh`, `ngrok` or `cloudflared`), supported platforms, and known limits such as GNOME's tray.
- Security notes: where secrets live, what `tunnels.toml` contains, that the file can be shared, and the update check.
- A short guide for the first tunnel, using screenshots from the native build.
- README's Design link points to a private artifact. Link to the checked-in `docs/devTTunel UI.html` instead.

## Done when

- A tag produces signed artifacts for all targets with no manual build steps.
- The checklist passes on clean machines, with no Gatekeeper or SmartScreen block for a signed build.
- The v1.0 exit test passes.
