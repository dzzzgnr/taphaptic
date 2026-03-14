#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/rotate-railway-secrets.sh \
    [--service agentwatch-api] \
    [--environment production] \
    --yes
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "missing required command: $1" >&2
    exit 127
  fi
}

require_cmd railway
require_cmd python3

service="agentwatch-api"
environment="production"
yes=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --service)
      service="${2:-}"
      shift 2
      ;;
    --environment)
      environment="${2:-}"
      shift 2
      ;;
    --yes)
      yes=1
      shift 1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '%s\n' "unknown argument: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if [ "$yes" -ne 1 ]; then
  printf '%s\n' "refusing to rotate secrets without --yes" >&2
  exit 64
fi

if ! railway whoami >/dev/null 2>&1; then
  printf '%s\n' "railway is not authenticated. run: railway login --browserless" >&2
  exit 69
fi

new_api_key="$(python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(36))
PY
)"

new_login_secret="$(python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(36))
PY
)"

railway variable set \
  AGENTWATCH_API_KEY="$new_api_key" \
  AGENTWATCH_LOGIN_SECRET="$new_login_secret" \
  --service "$service" \
  --environment "$environment" \
  --skip-deploys \
  --json >/dev/null

railway redeploy --service "$service" --yes >/dev/null

printf '%s\n' "Rotated AGENTWATCH_API_KEY and AGENTWATCH_LOGIN_SECRET."
printf '  service: %s (%s)\n' "$service" "$environment"
printf '%s\n' "  redeploy: triggered"
