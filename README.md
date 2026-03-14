# Taphaptic

Taphaptic sends Claude Code task status to Apple Watch using a local API running on your Mac.

## What this repo contains

- watchOS app (watch-only)
- local Go API for pairing + event ingestion
- Claude hook installer

## Requirements

- macOS with Xcode (Apple Watch deployment enabled)
- Go 1.22+
- Physical Apple Watch paired to iPhone

## Physical Watch Quickstart

1. Clone and bootstrap in one command:

```sh
git clone https://github.com/dzzzgnr/taphaptic.git && cd taphaptic && ./scripts/bootstrap-watch.sh
```

2. In Xcode, select scheme `Taphaptic`, choose your physical Apple Watch destination, and press Run.

3. Open Taphaptic on Apple Watch and enter the **4-digit** pairing code printed during bootstrap.

Bootstrap builds `taphaptic-api` and `taphapticctl` from local source.

## Daily Run

1. Start the API:

```sh
./scripts/start-api.sh
```

2. Start a new Claude session so hooks load.

3. Optional verification event:

```sh
./scripts/test-claude-connection.sh stop
```

4. Optional cleanup:

```sh
./scripts/stop-api.sh
```

## Uninstall

Remove Taphaptic API/runtime files from your Mac:

```sh
./scripts/uninstall.sh
```

Non-interactive mode (for CI/testing scripts):

```sh
./scripts/uninstall.sh --yes
```

To also restore the latest Claude settings backup made during install:

```sh
./scripts/uninstall.sh --yes --restore-claude-settings
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
