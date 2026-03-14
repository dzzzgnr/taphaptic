#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install Go 1.22+ from https://go.dev/dl/" >&2
  exit 127
fi

mkdir -p "$repo_root/bin"
(
  cd "$repo_root"
  go build -o "$repo_root/bin/taphapticctl" ./cmd/taphapticctl
)

if [ -n "${TAPHAPTIC_API_BASE_URL:-}" ]; then
  exec "$repo_root/bin/taphapticctl" install-consumer --api-base-url "$TAPHAPTIC_API_BASE_URL"
fi

exec "$repo_root/bin/taphapticctl" install-consumer
