#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
scope="user"

usage() {
  printf '%s\n' "usage: ./scripts/bootstrap-watch.sh [--scope user|project]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --scope)
      [ "$#" -ge 2 ] || {
        usage >&2
        exit 64
      }
      scope="$2"
      shift 2
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

"/bin/sh" "$repo_root/scripts/doctor.sh"

"/bin/sh" "$repo_root/scripts/start-api.sh"
"/bin/sh" "$repo_root/scripts/connect-claude-code.sh" --scope "$scope"

if ! open "$repo_root/Taphaptic.xcodeproj"; then
  printf '%s\n' "Warning: failed to open Xcode project automatically." >&2
fi

printf '\n'
printf '%s\n' "Next steps:"
printf '%s\n' "1. In Xcode, select scheme Taphaptic and your physical Apple Watch destination."
printf '%s\n' "2. Press Run to install the app."
printf '%s\n' "3. Open Taphaptic on the watch and enter the 4-digit code shown above."
