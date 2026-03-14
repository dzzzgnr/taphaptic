#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
port="${TAPHAPTIC_SMOKE_PORT:-18080}"
api_base_url="http://127.0.0.1:$port"

"$repo_root/scripts/build-taphaptic-api.sh"
ctl_path="$("$repo_root/scripts/ensure-binary.sh" taphapticctl)"

cd "$repo_root"
exec "$ctl_path" smoke-local-e2e --api-base-url "$api_base_url" --port "$port"
