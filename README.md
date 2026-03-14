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

## Local-only flow (same Wi-Fi)

1. Build the local API:

```sh
./scripts/build-taphaptic-api.sh
```

2. Run the API on your Mac:

```sh
./bin/taphaptic-api
```

3. Install Claude hooks and print a one-time pairing code:

```sh
./scripts/install-claude-hook.sh
```

4. Open Taphaptic on Apple Watch.

5. Wait for auto-discovery (Bonjour/mDNS) and enter the **4-digit** code.

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

## Watch app build

Build:

```sh
./scripts/build-watch-app.sh
```

Run on simulator:

```sh
./scripts/run-watch-sim.sh
```

Update all booted watch simulators:

```sh
./scripts/run-watch-sims.sh
```

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
