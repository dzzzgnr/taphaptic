# AgentWatch (Watch-Only Experiment)

AgentWatch delivers Claude status directly to Apple Watch with no iPhone app dependency in the active path.

## Consumer onboarding

1. In Terminal (macOS Terminal or iTerm) run:

```sh
curl -fsSL https://agentwatchapp.vercel.app/install/claude | sh
```

2. Installer prints a one-time 6-digit code.
3. Open AgentWatch on Apple Watch and enter the code.
4. Watch pairs and starts polling status directly from AgentWatch cloud.

No local backend. No user-entered backend URL. No QR/deeplink required.

## Architecture (experiment)

- Claude hook sends events to cloud backend:
  - `POST /v1/events` with scoped `claudeSessionToken`
- Claude installer generates watch pairing code:
  - `POST /v1/watch/pairings/code` with scoped `installationToken`
- Watch app claims code:
  - `POST /v1/watch/pairings/claim` and receives scoped `watchSessionToken`
- Watch app polls:
  - `GET /v1/events?since=<id>`

## Cloud API

Public:

- `GET /healthz`
- `POST /v1/auth/login` (legacy local fallback)
- `POST /v1/claude/installations`
- `POST /v1/watch/pairings/claim`

Authenticated:

- `POST /v1/watch/pairings/code`
- `POST /v1/events`
- `GET /v1/events?since=<id>`

Deprecated internal routes kept for rollback only:

- `/v1/pairings*` (legacy iPhone/QR flow)
- `/v1/status`
- `/v1/devices`

Token scopes:

- `installationToken`: bootstrap identity + create watch pairing code.
- `claudeSessionToken`: ingest events only.
- `watchSessionToken`: read events only.
- admin API key (`AGENTWATCH_API_KEY`): ops/testing override.

## Backend run (local)

Build:

```sh
./scripts/build-agentwatch-api.sh
```

Run:

```sh
AGENTWATCH_API_KEY=replace-me ./bin/agentwatch-api
```

Optional env:

- `PORT` (default `8080`)
- `AGENTWATCH_DATA_DIR`
- `AGENTWATCH_PUBLIC_API_BASE_URL`
- `AGENTWATCH_PAIR_BASE_URL` (legacy iPhone QR path)
- APNs env (optional): `AGENTWATCH_APNS_*`

Persisted backend files:

- `events.json`
- `devices.json`
- `sessions.json` (legacy)
- `channels.json`
- `claude_installations.json`
- `pairings.json` (legacy iPhone path)
- `watch_pairings.json`

## Claude setup (repo-local)

```sh
AGENTWATCH_API_BASE_URL="http://127.0.0.1:8080" sh ./scripts/install-claude-hook.sh
```

This installs helper + Claude hooks, bootstraps installation identity, stores Claude token, and prints a 6-digit watch pairing code.

## Watch app

Watch behavior:

- Distinct statuses: `Claude completed`, `Claude needs your attention`, `Claude subagent completed`, `Failed`, and `Pending`.
- In-app settings: sound on/off, haptic on/off, and reset pairing.
- Pairing UI uses a custom numeric keypad (no dictation keyboard).

Build:

```sh
./scripts/build-watch-app.sh
```

Run in simulator:

```sh
./scripts/run-watch-sim.sh
```

Update all booted watch simulators:

```sh
./scripts/run-watch-sims.sh
```

## iOS app status

iOS app code remains in the repository for rollback/reference, but it is not part of the active experiment build path.

- `./scripts/build-phone-app.sh` and `./scripts/run-phone-sim.sh` intentionally exit with guidance.
- Legacy iPhone pairing endpoints remain server-side.

## Operations

Health check + deployment validation:

```sh
./scripts/check-railway-production.sh --strict
```

`--strict` validates deployment health/logs for watch-only MVP. Add `--require-apns` only when APNs rollout is in scope.

Cloud smoke test (watch flow):

```sh
./scripts/smoke-cloud-e2e.sh
```

Hosted installer check:

```sh
./scripts/check-hosted-installer.sh
```

## Legacy local fallback (dev only)

The hook can still send to local macOS service only when explicitly enabled:

```sh
export AGENTWATCH_ALLOW_LEGACY_LOCAL=1
export AGENTWATCH_TOKEN=replace-me
```
