#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
  printf '%s\n' "usage: ./scripts/ensure-binary.sh <taphapticctl|taphaptic-api>" >&2
  exit 64
fi

tool="$1"
repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
bin_dir="$repo_root/bin"
mkdir -p "$bin_dir"

case "$tool" in
  taphapticctl)
    destination="$bin_dir/taphapticctl"
    source_pkg="./cmd/taphapticctl"
    ;;
  taphaptic-api)
    destination="$bin_dir/taphaptic-api"
    source_pkg="./cmd/taphaptic-api"
    ;;
  *)
    printf '%s\n' "unsupported tool: $tool (expected taphapticctl or taphaptic-api)" >&2
    exit 64
    ;;
esac

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install Go 1.22+ from https://go.dev/dl/" >&2
  exit 127
fi

(
  cd "$repo_root"
  go build -o "$destination" "$source_pkg"
)
chmod 755 "$destination"
printf '%s\n' "$destination"
