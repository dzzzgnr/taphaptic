#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
scope="user"
with_notifications="0"

usage() {
  printf '%s\n' "usage: sh scripts/patch-claude-settings.sh [--scope user|project] [--with-notifications]"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --scope)
      [ $# -ge 2 ] || {
        usage >&2
        exit 64
      }
      scope="$2"
      shift 2
      ;;
    --with-notifications)
      with_notifications="1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 64
      ;;
  esac
done

case "$scope" in
  user|project)
    ;;
  *)
    printf '%s\n' "unsupported scope: $scope (use user or project)" >&2
    exit 64
    ;;
esac

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install Go 1.22+ from https://go.dev/dl/" >&2
  exit 127
fi

mkdir -p "$repo_root/bin"
(
  cd "$repo_root"
  go build -o "$repo_root/bin/taphapticctl" ./cmd/taphapticctl
)

cd "$repo_root"

if [ "$with_notifications" = "1" ]; then
  exec "$repo_root/bin/taphapticctl" patch-settings --scope "$scope" --with-notifications
fi

exec "$repo_root/bin/taphapticctl" patch-settings --scope "$scope"
