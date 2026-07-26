#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
scope="user"
provider="all"

usage() {
  printf '%s\n' "usage: ./scripts/connect-agents.sh [--scope user|project] [--provider claude|codex|all]"
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
    --provider)
      [ "$#" -ge 2 ] || {
        usage >&2
        exit 64
      }
      provider="$2"
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

case "$provider" in
  claude|codex|all)
    ;;
  *)
    printf '%s\n' "unsupported provider: $provider (use claude, codex, or all)" >&2
    exit 64
    ;;
esac

ctl_path="$("$repo_root/scripts/ensure-binary.sh" taphapticctl)"

if [ -n "${TAPHAPTIC_API_BASE_URL:-}" ]; then
  "$ctl_path" install-consumer \
    --provider "$provider" \
    --scope "$scope" \
    --api-base-url "$TAPHAPTIC_API_BASE_URL"
else
  "$ctl_path" install-consumer --provider "$provider" --scope "$scope"
fi

printf '%s\n' "Taphaptic onboarding is ready for provider: $provider."
printf '%s\n' "Start new Claude Code and Codex tasks so updated hooks are loaded."
