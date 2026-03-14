#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
port="${TAPHAPTIC_SMOKE_PORT:-18080}"
api_base_url="http://127.0.0.1:$port"

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install Go 1.22+ from https://go.dev/dl/" >&2
  exit 127
fi

"$repo_root/scripts/build-taphaptic-api.sh"
mkdir -p "$repo_root/bin"
(
  cd "$repo_root"
  go build -o "$repo_root/bin/taphapticctl" ./cmd/taphapticctl
)

cd "$repo_root"
exec "$repo_root/bin/taphapticctl" smoke-local-e2e --api-base-url "$api_base_url" --port "$port"
