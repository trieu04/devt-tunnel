# Release checklist

1. Confirm the tag equals `info.version` in `build/config.yml`; the release workflow rejects a mismatch.
2. Require green Go, frontend and native packaging jobs for macOS arm64/x64, Windows amd64 and Linux amd64.
3. Download each draft artifact on a clean matching machine. Signed releases must open without Gatekeeper or SmartScreen warnings; artifacts named `-unsigned` are tester-only.
4. Create an SSH tunnel from a config host, start it, connect through the forward, stop it and quit; confirm no process remains.
5. Upgrade from the previous release and confirm `tunnels.toml`, `settings.toml`, `state.json` and keychain secrets survive.
6. Uninstall and confirm application files are removed while user definitions, settings, state and secrets remain.
7. Record installer size and idle memory for each OS in `docs/platform.md`, then publish the draft release.
8. Update the Homebrew tap after the notarized macOS artifact is public.

The release workflow can produce unsigned tester artifacts without signing secrets. Publishing v1 requires an Apple Developer identity, Apple notarization API key, a Windows signing certificate and a project license selected by the owner.
