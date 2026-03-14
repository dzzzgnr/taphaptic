# Contributing

## Prerequisites

- Go 1.22+
- Xcode + watchOS simulator runtime
- `xcodegen`

## Local development

1. Run backend tests:

```sh
go test ./...
```

2. Build local API:

```sh
./scripts/build-agentwatch-api.sh
```

3. Build watch app:

```sh
./scripts/build-watch-app.sh
```

## Scope guardrails

- Local-only architecture is the default.
- Keep watch + local API pairing flow stable.
- Do not reintroduce hosted/cloud defaults without explicit maintainer approval.
