#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
scope="user"

usage() {
  printf '%s\n' "usage: sh scripts/connect-claude-code.sh [--scope user|project]"
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

ctl_path="$("$repo_root/scripts/ensure-binary.sh" taphapticctl)"
if [ -n "${TAPHAPTIC_API_BASE_URL:-}" ]; then
  "$ctl_path" install-consumer --provider claude --scope "$scope" --api-base-url "$TAPHAPTIC_API_BASE_URL"
else
  "$ctl_path" install-consumer --provider claude --scope "$scope"
fi

printf '%s\n' "Taphaptic onboarding is ready for Claude."
printf '%s\n' "Installer prints a 4-digit pairing code for Apple Watch."
printf '%s\n' "Start a new Claude session so updated hooks are loaded."
