#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/smoke-cloud-e2e.sh \
    [--api-base-url https://agentwatch-api-production-39a1.up.railway.app]
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "missing required command: $1" >&2
    exit 127
  fi
}

require_cmd curl
require_cmd python3

api_base_url="https://agentwatch-api-production-39a1.up.railway.app"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --api-base-url)
      api_base_url="${2:-}"
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

json_field() {
  key="$1"
  payload="$2"
  printf '%s' "$payload" | python3 -c '
import json, sys
key = sys.argv[1]
try:
    data = json.load(sys.stdin)
except Exception:
    print("")
    raise SystemExit(0)
value = data.get(key, "")
if value is None:
    value = ""
print(value)
' "$key"
}

json_nested() {
  payload="$1"
  expr="$2"
  printf '%s' "$payload" | python3 -c '
import json, sys
expr = sys.argv[1]
data = json.load(sys.stdin)
namespace = {"data": data}
allowed = {"len": len, "next": next, "str": str}
try:
    value = eval(expr, {"__builtins__": allowed}, namespace)
except Exception:
    print("")
    raise SystemExit(0)
if value is None:
    value = ""
print(value)
' "$expr"
}

api_base_url="${api_base_url%/}"

health_code="$(curl -fsS -o /dev/null -w '%{http_code}' "$api_base_url/healthz" || true)"
if [ "$health_code" != "204" ]; then
  printf '%s\n' "health check failed: expected 204, got $health_code" >&2
  exit 70
fi

install_response="$(curl -fsS -X POST -H "Content-Type: application/json" -d '{}' "$api_base_url/v1/claude/installations")"
installation_token="$(json_field "installationToken" "$install_response")"
installation_id="$(json_field "installationId" "$install_response")"
claude_session_token="$(json_field "claudeSessionToken" "$install_response")"

if [ -z "$installation_token" ] || [ -z "$installation_id" ] || [ -z "$claude_session_token" ]; then
  printf '%s\n' "installation bootstrap failed" >&2
  exit 70
fi

watch_code_response="$(curl -fsS -X POST \
  -H "Authorization: Bearer $installation_token" \
  -H "Content-Type: application/json" \
  -d '{}' \
  "$api_base_url/v1/watch/pairings/code")"

watch_code="$(json_field "code" "$watch_code_response")"
if [ -z "$watch_code" ]; then
  printf '%s\n' "watch pairing code creation failed" >&2
  exit 70
fi

watch_installation_id="$(python3 - <<'PY'
import uuid
print("watch-" + str(uuid.uuid4()).lower())
PY
)"

watch_claim_response="$(curl -fsS -X POST \
  -H "Content-Type: application/json" \
  -d "{\"code\":\"$watch_code\",\"watchInstallationId\":\"$watch_installation_id\"}" \
  "$api_base_url/v1/watch/pairings/claim")"

watch_session_token="$(json_field "watchSessionToken" "$watch_claim_response")"
channel_id="$(json_field "channelId" "$watch_claim_response")"
if [ -z "$watch_session_token" ] || [ -z "$channel_id" ]; then
  printf '%s\n' "watch pairing claim failed" >&2
  exit 70
fi

event_title="E2E $(date -u +%Y%m%dT%H%M%SZ)"
event_body="smoke-cloud-e2e-watch"

event_response="$(curl -fsS -X POST \
  -H "Authorization: Bearer $claude_session_token" \
  -H "Content-Type: application/json" \
  -d "{\"type\":\"completed\",\"source\":\"smoke-cloud-e2e\",\"title\":\"$event_title\",\"body\":\"$event_body\"}" \
  "$api_base_url/v1/events")"

event_id="$(json_nested "$event_response" 'data.get("event", {}).get("id", "")')"
if [ -z "$event_id" ]; then
  printf '%s\n' "event ingest failed" >&2
  exit 70
fi

events_response="$(curl -fsS \
  -H "Authorization: Bearer $watch_session_token" \
  "$api_base_url/v1/events?since=0")"

matched_count="$(json_nested "$events_response" 'len([e for e in data.get("events", []) if str(e.get("id", "")) == "'"$event_id"'"])')"
if [ "$matched_count" = "0" ]; then
  printf '%s\n' "events feed missing ingested event id=$event_id" >&2
  exit 70
fi

matched_title="$(json_nested "$events_response" 'next((e.get("title", "") for e in data.get("events", []) if str(e.get("id", "")) == "'"$event_id"'"), "")')"
if [ "$matched_title" != "$event_title" ]; then
  printf '%s\n' "events feed title mismatch: expected '$event_title', got '$matched_title'" >&2
  exit 70
fi

printf '%s\n' "Cloud smoke E2E passed."
printf '  api: %s\n' "$api_base_url"
printf '  installation: %s\n' "$installation_id"
printf '  channel: %s\n' "$channel_id"
printf '  watch_installation: %s\n' "$watch_installation_id"
printf '  event_id: %s\n' "$event_id"
