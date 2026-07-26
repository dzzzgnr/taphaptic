# Taphaptic

Taphaptic sends Claude Code and Codex lifecycle notifications to an independent
Apple Watch app.

## Architecture

- Claude Code and Codex hooks run a small `taphapticctl` adapter on the user's Mac.
- The adapter sends a minimal, provider-neutral event to the HTTPS relay.
- The relay sends a standard alert directly to the watch-only app through APNs.
- A local Bonjour/HTTP mode remains available for development and self-hosting.

Prompts, transcripts, repository files, tool arguments, and generated answers
are not sent by the built-in hooks.

## Requirements

- macOS with Go 1.22+ for building the Mac adapter
- Xcode 26+ with the watchOS 26 platform installed for App Store builds
- Apple Watch running watchOS 11 or later
- A relay URL for background notifications

## Connect Claude Code, Codex, or both

Build/start a local relay for development:

```sh
./scripts/start-api.sh
```

Install both adapters and print a watch pairing code:

```sh
./scripts/connect-agents.sh --provider all --scope user
```

Provider-specific alternatives:

```sh
./scripts/connect-claude-code.sh --scope user
./scripts/connect-codex.sh --scope user
```

For a hosted relay:

```sh
./scripts/connect-agents.sh \
  --provider all \
  --scope user \
  --api-base-url https://your-relay.example
```

Codex requires review of new command hooks. Open `/hooks` in Codex and trust the
Taphaptic entries after inspecting them. Taphaptic never bypasses hook trust.

## Apple Watch setup

1. Install and launch Taphaptic on Apple Watch.
2. Enter the four-digit code printed by the Mac setup command.
3. Allow notifications when prompted.
4. Open Settings in Taphaptic and choose **Send test notification**.

The hosted/APNs path works when Taphaptic is not open. The local HTTP fallback
polls only while the watch app is active.

## Relay configuration

The container listens on `PORT` and persists state under `TAPHAPTIC_DATA_DIR`.
Production APNs requires:

```text
TAPHAPTIC_APNS_KEY_ID
TAPHAPTIC_APNS_TEAM_ID
TAPHAPTIC_APNS_TOPIC
TAPHAPTIC_APNS_PRIVATE_KEY_PATH
# or TAPHAPTIC_APNS_PRIVATE_KEY with the PEM value from a secret manager
TAPHAPTIC_APNS_ENVIRONMENT=production
```

Set `TAPHAPTIC_TRUST_PROXY_HEADERS=true` only when the service is behind a
trusted reverse proxy that overwrites `X-Forwarded-For`.

Never put the APNs private key in the repository, image, watch app, or logs.
Mount it from the host's secret manager.

`render.yaml` defines a one-instance Frankfurt deployment with an encrypted
1 GB persistent disk, daily disk snapshots, HTTPS, health checks, and
deploy-after-CI. Render prompts for the APNs key ID and private key during the
first Blueprint sync; the values are not stored in Git.

## API

Public:

- `GET /healthz`
- `POST /v1/installations`
- `POST /v1/claude/installations` (one-release compatibility alias)
- `POST /v1/watch/pairings/claim`

Authenticated:

- `DELETE /v1/installations/current` (installation token)
- `POST /v1/watch/pairings/code` (installation token)
- `POST /v1/events` (producer token)
- `GET /v1/events?since=<id>` (watch token)
- `POST /v1/watch/devices` (watch token)
- `DELETE /v1/watch/devices` (watch token)
- `POST /v1/watch/test-notification` (watch token)

## Verification

```sh
go test ./... -count=1
go test -race ./... -count=1
./scripts/smoke-local-e2e.sh
./scripts/test-shared-swift.sh
./scripts/build-watch-app.sh
```

The watch build requires the watchOS platform component, not only the SDK
stubs bundled with Xcode.

## Uninstall and deletion

```sh
./scripts/uninstall.sh --yes
```

Uninstall first asks the configured relay to delete the installation, device
registrations, event records, and all scoped tokens. It then removes only
Taphaptic's Claude Code/Codex hook entries and local runtime files.

See the [privacy policy](docs/privacy-policy.md), [support guide](docs/support.md),
and [release checklist](docs/app-store-release-checklist.md).
