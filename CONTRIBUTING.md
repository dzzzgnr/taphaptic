# Contributing

## Setup

- Go 1.22+
- Xcode (Apple platform toolchain)
- Physical Apple Watch paired to iPhone (required only for on-device watch validation)

## First-time local setup

```sh
./scripts/bootstrap-watch.sh
```

This runs preflight checks, starts the API, and installs Claude hooks.

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
