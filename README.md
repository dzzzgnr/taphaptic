# Taphaptic

Taphaptic sends Claude Code task status to Apple Watch using a local API running on your Mac.

## What this repo contains

- watchOS app (watch-only)
- local Go API for pairing + event ingestion
- Claude hook installer

## Requirements

- macOS with Xcode (Apple Watch deployment enabled)
- Go 1.22+
- `curl`
- `python3`
- Physical Apple Watch paired to iPhone

## Physical Watch Quickstart

1. Clone the repo:

```sh
git clone https://github.com/dzzzgnr/taphaptic.git && cd taphaptic
```

2. Build the local API:

```sh
./scripts/build-taphaptic-api.sh
```

3. Run the API on your Mac (keep this terminal open):

```sh
./bin/taphaptic-api
```

4. In another terminal, install Claude hooks and generate a pairing code:

```sh
./scripts/install-claude-hook.sh
```

5. Open `Taphaptic.xcodeproj` in Xcode, select scheme `Taphaptic`, choose your physical Apple Watch destination, and press Run.

6. Open Taphaptic on Apple Watch and enter the **4-digit** code from step 4.

## Daily Run

1. Start the API:

```sh
./bin/taphaptic-api
```

2. Start a new Claude session.

3. Optional verification event:

```sh
./scripts/test-claude-connection.sh stop
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

Run shared Swift regression tests:

```sh
./scripts/test-shared-swift.sh
```

## Notes

- Watch and Mac must be on the same local network.
- If discovery fails, keep the API running and retry pairing from watch.
