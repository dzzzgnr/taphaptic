# Taphaptic

Taphaptic sends Claude Code task status to Apple Watch using a local API running on your Mac.

## What this repo contains

- watchOS app (watch-only)
- local Go API for pairing + event ingestion
- Claude hook installer

Out of scope in v1:

- hosted/cloud relay flow
- iPhone companion flow
- legacy macOS token service

## Prerequisites (manual)

- macOS with Xcode and watchOS runtime
- Go 1.22+
- Physical Apple Watch paired to iPhone (for device deployment)

## First-time setup (physical watch)

```sh
git clone <repo-url> && cd taphaptic && ./scripts/bootstrap-watch.sh
```

`./scripts/bootstrap-watch.sh` will:

1. Run preflight checks (`doctor`).
2. Start local API.
3. Install Claude hooks and print a 4-digit watch pairing code.
4. Open `Taphaptic.xcodeproj`.

Then complete these manual steps:

1. In Xcode, select scheme `Taphaptic` and your physical Apple Watch destination.
2. Press Run to install the app.
3. Open Taphaptic on Apple Watch and enter the **4-digit** code.

## Daily run

1. Start local API:

```sh
./scripts/start-api.sh
```

2. Start a new Claude session (so hooks are loaded).

3. Optional verification event:

```sh
./scripts/test-claude-connection.sh stop
```

4. Optional cleanup:

```sh
./scripts/stop-api.sh
```

## Simulator flow (optional)

Build for watch simulator:

```sh
./scripts/build-watch-app.sh
```

Run on one simulator:

```sh
./scripts/run-watch-sim.sh
```

Update all booted watch simulators:

```sh
./scripts/run-watch-sims.sh
```

## How pairing works

- Installer calls `POST /v1/claude/installations` to bootstrap installation identity.
- Installer calls `POST /v1/watch/pairings/code` to generate a 4-digit code.
- Watch app auto-discovers the local API on LAN (`_taphaptic._tcp`) and claims code via `POST /v1/watch/pairings/claim`.
- Claude hooks send events with `POST /v1/events`.
- Watch polls events with `GET /v1/events?since=<id>`.

## Local API

Public routes:

- `GET /healthz`
- `POST /v1/claude/installations`
- `POST /v1/watch/pairings/claim`

Authenticated routes:

- `POST /v1/watch/pairings/code` (installation token)
- `POST /v1/events` (claude session token)
- `GET /v1/events?since=<id>` (watch session token)

## CI checks (local equivalent)

Run backend unit tests:

```sh
go test ./... -count=1
```

Run API smoke e2e (installation -> pairing -> claim -> event -> poll):

```sh
./scripts/smoke-local-e2e.sh
```

Run Apple/watch checks:

```sh
./scripts/build-watch-app.sh
./scripts/test-shared-swift.sh
```

## Notes

- Watch and Mac must be on the same local network.
- If discovery fails, keep the API running and retry pairing from watch.
