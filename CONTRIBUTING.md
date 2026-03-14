# Contributing

## Setup

- Xcode (Apple platform toolchain)
- Physical Apple Watch paired to iPhone (required only for on-device watch validation)

## First-time local setup

```sh
./scripts/bootstrap-watch.sh
```

This runs preflight checks, starts the API, and installs Claude hooks.

Default scripts download prebuilt `taphaptic-api` and `taphapticctl` binaries from GitHub Releases when missing.

## Developer mode

- Go 1.22+ (for local source builds and backend tests)
- Set `TAPHAPTIC_DEV_MODE=1` to force scripts to build binaries from local source instead of downloading prebuilt assets.
- Use developer mode when testing unreleased branches before release assets exist.
- Publishing a GitHub Release triggers `.github/workflows/release-binaries.yml` to build, sign, notarize, and attach macOS binaries.

## Physical-watch release gate

- Before tagging any release, complete the physical-watch checklist in [docs/physical-watch-validation.md](docs/physical-watch-validation.md).
- Paste the completed pass/fail template from that checklist into the release PR notes.

### Release signing and notarization

Configure these repository secrets before publishing a release:

- `MACOS_SIGNING_CERT_BASE64` (base64-encoded `.p12` Developer ID Application certificate)
- `MACOS_SIGNING_CERT_PASSWORD` (password for the `.p12` certificate)
- `MACOS_SIGNING_IDENTITY` (for example `Developer ID Application: Example, Inc. (TEAMID)`)
- `APPLE_ID` (Apple account email for notarization)
- `APPLE_APP_SPECIFIC_PASSWORD` (app-specific password for `APPLE_ID`)
- `APPLE_TEAM_ID` (Apple Developer Team ID)

Local verification commands for signed artifacts:

```sh
codesign --verify --strict --verbose=2 ./bin/taphapticctl
spctl --assess --type execute --verbose=4 ./bin/taphapticctl

codesign --verify --strict --verbose=2 ./bin/taphaptic-api
spctl --assess --type execute --verbose=4 ./bin/taphaptic-api
```

## Legacy installer

- `deploy/legacy/claude-consumer-installer.sh` is kept for historical reference only.
- Default onboarding and support path is `./scripts/install-claude-hook.sh` (via `taphapticctl`).

## Daily local run

1. Start API:

```sh
./scripts/start-api.sh
```

2. Run checks you need:

```sh
go test ./... -count=1
./scripts/build-watch-app.sh
./scripts/test-shared-swift.sh
```

3. Optional API cleanup:

```sh
./scripts/stop-api.sh
```

## Scope guardrails

- Local-only architecture is the default.
- Keep watch + local API pairing flow stable.
- Do not reintroduce hosted/cloud defaults without explicit maintainer approval.
