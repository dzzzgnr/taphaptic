#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
ctl_path="$("$repo_root/scripts/ensure-binary.sh" taphapticctl)"

if [ -n "${TAPHAPTIC_API_BASE_URL:-}" ]; then
  exec "$ctl_path" install-consumer --api-base-url "$TAPHAPTIC_API_BASE_URL"
fi

exec "$ctl_path" install-consumer
