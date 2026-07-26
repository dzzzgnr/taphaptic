# Contributing

## Setup

- Go 1.22+
- Xcode (Apple platform toolchain)
- Physical Apple Watch paired to iPhone (required only for on-device watch validation)

## First-time local setup

```sh
./scripts/bootstrap-watch.sh
```

This runs preflight checks, starts the local API, and installs Claude Code and
Codex hooks.

## Physical-watch release gate

- Before tagging any release, complete the physical-watch checklist in [docs/physical-watch-validation.md](docs/physical-watch-validation.md).
- Paste the completed pass/fail template from that checklist into the release PR notes.

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

- The production architecture is an HTTPS relay with direct APNs delivery.
- Keep the local Bonjour/HTTP mode working as a development and self-hosted fallback.
- Never commit APNs keys, App Store Connect keys, bearer tokens, or provisioning profiles.
- Preserve unrelated Claude Code and Codex hook configuration exactly.
