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

/bin/sh "$repo_root/scripts/install-claude-hook.sh"

if [ "$scope" = "project" ]; then
  /bin/sh "$repo_root/scripts/patch-claude-settings.sh" --scope project --with-notifications >/dev/null
fi

printf '%s\n' "Taphaptic onboarding is ready for Claude."
printf '%s\n' "Installer prints a 4-digit pairing code for Apple Watch."
printf '%s\n' "Start a new Claude session so updated hooks are loaded."
