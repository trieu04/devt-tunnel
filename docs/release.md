# Release checklist

1. Confirm the tag equals `info.version` in `build/config.yml`; the release workflow rejects a mismatch.
2. Refresh the binary manifest with `task downloads:manifest` and review the diff, or note in the release why the pinned `cloudflared` stays ([binaries design](design/binaries.md#manifest)).
3. Require green Go, frontend and native packaging jobs for macOS arm64/x64, Windows amd64 and Linux amd64.
4. Download each draft artifact on a clean matching machine. Signed releases must open without Gatekeeper or SmartScreen warnings; artifacts named `-unsigned` are tester-only.
5. Create an SSH tunnel from a config host, start it, connect through the forward, stop it and quit; confirm no process remains.
6. Upgrade from the previous release and confirm `tunnels.toml`, `settings.toml`, `state.json` and keychain secrets survive.
7. Uninstall and confirm application files are removed while user definitions, settings, state and secrets remain.
8. Record installer size and idle memory for each OS in `docs/platform.md`, then publish the draft release.
9. Update the Homebrew tap after the notarized macOS artifact is public.
10. On the first public release, update `README.md`: the status note, the Installation section with download links, and a release version badge.

The release workflow can produce unsigned tester artifacts without signing secrets. Publishing v1 requires an Apple Developer identity, Apple notarization API key and a Windows signing certificate.
