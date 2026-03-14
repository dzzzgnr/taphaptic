#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
output_dir="$repo_root/bin"
output_path="$output_dir/agentwatch"

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install it with: brew install go" >&2
  exit 127
fi

mkdir -p "$output_dir"

exec go build -o "$output_path" ./cmd/agentwatch
