# Contributing

## Prerequisites

- Go 1.22+
- Xcode + watchOS simulator runtime

## First-time local setup

```sh
./scripts/bootstrap-watch.sh
```

This runs preflight checks, starts the API, and installs Claude hooks.

## Maintainer tooling

- Regenerate project files from `project.yml` only when needed:

```sh
./scripts/regenerate-xcodeproj.sh
```

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
