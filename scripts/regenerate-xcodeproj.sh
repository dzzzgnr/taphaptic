#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

if ! command -v xcodegen >/dev/null 2>&1; then
  printf '%s\n' "xcodegen is required for project regeneration. Install it with: brew install xcodegen" >&2
  exit 127
fi

cd "$repo_root"
exec xcodegen generate
