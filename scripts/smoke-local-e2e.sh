#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
api_bin="$repo_root/bin/taphaptic-api"

if ! command -v curl >/dev/null 2>&1; then
  printf '%s\n' "curl is required." >&2
  exit 127
fi

if ! command -v python3 >/dev/null 2>&1; then
  printf '%s\n' "python3 is required." >&2
  exit 127
fi

if [ ! -x "$api_bin" ]; then
  printf '%s\n' "API binary not found at $api_bin. Build it first." >&2
  exit 66
fi

tmp_dir="$(mktemp -d)"
port="${TAPHAPTIC_SMOKE_PORT:-18080}"
api_base_url="http://127.0.0.1:$port"
api_log="$tmp_dir/api.log"

cleanup() {
  if [ "${api_pid:-}" != "" ] && kill -0 "$api_pid" >/dev/null 2>&1; then
    kill "$api_pid" >/dev/null 2>&1 || true
    wait "$api_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

json_get() {
  key="$1"
  payload="$2"
  printf '%s' "$payload" | python3 -c '
import json
import sys

key = sys.argv[1]
data = json.load(sys.stdin)
value = data.get(key, "")
if value is None:
    value = ""
print(value)
' "$key"
}

json_count_events() {
  payload="$1"
  printf '%s' "$payload" | python3 -c '
import json
import sys

data = json.load(sys.stdin)
events = data.get("events", [])
print(len(events))
'
}

json_first_event_type() {
  payload="$1"
  printf '%s' "$payload" | python3 -c '
import json
import sys

data = json.load(sys.stdin)
events = data.get("events", [])
if not events:
    print("")
else:
    print(events[0].get("type", ""))
'
}

TAPHAPTIC_BIND_HOST="127.0.0.1" \
TAPHAPTIC_PORT="$port" \
TAPHAPTIC_DATA_DIR="$tmp_dir/data" \
"$api_bin" >"$api_log" 2>&1 &
api_pid="$!"

for _ in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS --max-time 2 "$api_base_url/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

if ! curl -fsS --max-time 2 "$api_base_url/healthz" >/dev/null 2>&1; then
  printf '%s\n' "API failed health check. Logs:" >&2
  cat "$api_log" >&2 || true
  exit 70
fi

installation_response="$(curl -fsS -X POST "$api_base_url/v1/claude/installations" \
  -H "Content-Type: application/json" \
  -d '{}')"

installation_token="$(json_get "installationToken" "$installation_response")"
installation_id="$(json_get "installationId" "$installation_response")"
claude_session_token="$(json_get "claudeSessionToken" "$installation_response")"

if [ -z "$installation_token" ] || [ -z "$installation_id" ] || [ -z "$claude_session_token" ]; then
  printf '%s\n' "Invalid installation response: $installation_response" >&2
  exit 70
fi

pairing_response="$(curl -fsS -X POST "$api_base_url/v1/watch/pairings/code" \
  -H "Authorization: Bearer $installation_token" \
  -H "Content-Type: application/json" \
  -d '{}')"

pairing_code="$(json_get "code" "$pairing_response")"
if [ -z "$pairing_code" ]; then
  printf '%s\n' "Invalid pairing response: $pairing_response" >&2
  exit 70
fi

claim_response="$(curl -fsS -X POST "$api_base_url/v1/watch/pairings/claim" \
  -H "Content-Type: application/json" \
  -d "{\"code\":\"$pairing_code\",\"watchInstallationId\":\"watch-smoke\"}")"

watch_session_token="$(json_get "watchSessionToken" "$claim_response")"
if [ -z "$watch_session_token" ]; then
  printf '%s\n' "Invalid claim response: $claim_response" >&2
  exit 70
fi

curl -fsS -X POST "$api_base_url/v1/events" \
  -H "Authorization: Bearer $claude_session_token" \
  -H "Content-Type: application/json" \
  -d '{"type":"completed","source":"smoke","title":"smoke","body":"smoke"}' >/dev/null

events_response="$(curl -fsS -X GET "$api_base_url/v1/events?since=0" \
  -H "Authorization: Bearer $watch_session_token")"

events_count="$(json_count_events "$events_response")"
first_event_type="$(json_first_event_type "$events_response")"

if [ "$events_count" -lt 1 ] || [ "$first_event_type" != "completed" ]; then
  printf '%s\n' "Invalid events response: $events_response" >&2
  exit 70
fi

printf '%s\n' "Smoke E2E passed (installation -> pairing -> claim -> event -> poll)."
