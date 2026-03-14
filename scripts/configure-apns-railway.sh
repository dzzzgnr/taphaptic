#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/configure-apns-railway.sh \
    --key-file /absolute/path/AuthKey_XXXXXX.p8 \
    --key-id XXXXXX \
    [--team-id X53HKYK69N] \
    [--topic local.agentwatch.phone] \
    [--sandbox false] \
    [--service agentwatch-api] \
    [--environment production]
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "missing required command: $1" >&2
    exit 127
  fi
}

require_cmd railway
require_cmd base64

key_file=""
key_id=""
team_id="X53HKYK69N"
topic="local.agentwatch.phone"
sandbox="false"
service="agentwatch-api"
environment="production"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --key-file)
      key_file="${2:-}"
      shift 2
      ;;
    --key-id)
      key_id="${2:-}"
      shift 2
      ;;
    --team-id)
      team_id="${2:-}"
      shift 2
      ;;
    --topic)
      topic="${2:-}"
      shift 2
      ;;
    --sandbox)
      sandbox="${2:-}"
      shift 2
      ;;
    --service)
      service="${2:-}"
      shift 2
      ;;
    --environment)
      environment="${2:-}"
      shift 2
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

if [ -z "$key_file" ] || [ -z "$key_id" ]; then
  usage >&2
  exit 64
fi

if [ ! -f "$key_file" ]; then
  printf '%s\n' "key file not found: $key_file" >&2
  exit 66
fi

case "$sandbox" in
  true|false|1|0)
    ;;
  *)
    printf '%s\n' "--sandbox must be true/false/1/0, got: $sandbox" >&2
    exit 64
    ;;
esac

if ! railway whoami >/dev/null 2>&1; then
  printf '%s\n' "railway is not authenticated. run: railway login --browserless" >&2
  exit 69
fi

key_filename="$(basename "$key_file")"
remote_key_path="/data/$key_filename"
key_b64="$(base64 < "$key_file" | tr -d '\n')"

printf '%s\n' "Uploading APNs key to Railway volume: $remote_key_path"
railway ssh --service "$service" --environment "$environment" -- \
  "sh -lc 'printf %s \"$key_b64\" | base64 -d > \"$remote_key_path\" && chmod 600 \"$remote_key_path\" && ls -l \"$remote_key_path\"'"

printf '%s\n' "Setting APNs variables on Railway service: $service ($environment)"
railway variable set \
  AGENTWATCH_APNS_TEAM_ID="$team_id" \
  AGENTWATCH_APNS_KEY_ID="$key_id" \
  AGENTWATCH_APNS_TOPIC="$topic" \
  AGENTWATCH_APNS_PRIVATE_KEY_PATH="$remote_key_path" \
  AGENTWATCH_APNS_SANDBOX="$sandbox" \
  --service "$service" \
  --environment "$environment" \
  --json >/dev/null

printf '%s\n' "Redeploying service with APNs enabled..."
railway redeploy --service "$service" --environment "$environment" >/dev/null

printf '%s\n' "Done. APNs config applied."
printf '%s\n' "  team:    $team_id"
printf '%s\n' "  key id:  $key_id"
printf '%s\n' "  topic:   $topic"
printf '%s\n' "  sandbox: $sandbox"
printf '%s\n' "  key:     $remote_key_path"
