#!/bin/sh

set -eu

action="${1:-}"
install_root="$HOME/Library/Application Support/AgentWatch"

api_base_url="${AGENTWATCH_API_BASE_URL:-}"
api_base_url_file="$install_root/api-base-url"

claude_session_token="${AGENTWATCH_CLAUDE_SESSION_TOKEN:-}"
claude_session_token_file="$install_root/claude-session-token"
installation_token_file="$install_root/installation-token"

legacy_token="${AGENTWATCH_TOKEN:-}"
legacy_token_file="$install_root/token"
legacy_base_url="${AGENTWATCH_BASE_URL:-http://127.0.0.1:7878}"

read_file_if_empty() {
  current_value="$1"
  path="$2"
  if [ -z "$current_value" ] && [ -f "$path" ]; then
    cat "$path"
  else
    printf '%s' "$current_value"
  fi
}

save_secret_file() {
  path="$1"
  value="$2"
  mkdir -p "$(dirname "$path")"
  printf '%s' "$value" > "$path"
  chmod 600 "$path"
}

json_field() {
  key="$1"
  payload="$2"
  if ! command -v python3 >/dev/null 2>&1; then
    printf '%s' ""
    return 0
  fi

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
if isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
' "$key"
}

resolve_session_token_from_installation() {
  installation_token="$(read_file_if_empty "" "$installation_token_file")"
  if [ -z "$installation_token" ]; then
    printf '%s' ""
    return 0
  fi

  installation_response="$(curl -fsS -m 2 -X POST \
    -H "Authorization: Bearer $installation_token" \
    -H "Content-Type: application/json" \
    -d '{}' \
    "${api_base_url%/}/v1/claude/installations" || true)"

  resolved_token="$(json_field "claudeSessionToken" "$installation_response")"
  if [ -n "$resolved_token" ]; then
    save_secret_file "$claude_session_token_file" "$resolved_token"
    printf '%s' "$resolved_token"
    return 0
  fi

  printf '%s' ""
}

case "$action" in
  stop|completed)
    event_type="completed"
    title="Claude Code completed"
    body="AGENT COMPLETED A TASK"
    legacy_endpoint="complete"
    ;;
  subagent_stop|subagent_completed)
    event_type="subagent_completed"
    title="Claude subagent completed"
    body="Claude Code subagent finished background work"
    legacy_endpoint="complete"
    ;;
  failed)
    event_type="failed"
    title="Claude Code failed"
    body="Claude Code reported a failure"
    legacy_endpoint="failed"
    ;;
  permission_prompt)
    event_type="attention"
    title="Claude Code needs permission"
    body="Claude Code is waiting for permission"
    legacy_endpoint="attention"
    ;;
  idle_prompt)
    event_type="attention"
    title="Claude Code is waiting"
    body="Claude Code is idle and waiting for input"
    legacy_endpoint="attention"
    ;;
  attention)
    event_type="attention"
    title="Claude Code needs attention"
    body="Claude Code needs attention"
    legacy_endpoint="attention"
    ;;
  *)
    printf '%s\n' "usage: /bin/sh agentwatch-hook stop|subagent_stop|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention" >&2
    exit 64
    ;;
esac

api_base_url="$(read_file_if_empty "$api_base_url" "$api_base_url_file")"
claude_session_token="$(read_file_if_empty "$claude_session_token" "$claude_session_token_file")"

if [ -z "$api_base_url" ]; then
  api_base_url="https://agentwatch-api-production-39a1.up.railway.app"
fi

if [ -z "$claude_session_token" ]; then
  claude_session_token="$(resolve_session_token_from_installation)"
fi

if [ -n "$claude_session_token" ]; then
  payload="$(printf '{"type":"%s","source":"claude-code","title":"%s","body":"%s"}' "$event_type" "$title" "$body")"
  curl -fsS -m 3 -X POST \
    -H "Authorization: Bearer $claude_session_token" \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "${api_base_url%/}/v1/events" >/dev/null 2>&1 &
  exit 0
fi

if [ "${AGENTWATCH_ALLOW_LEGACY_LOCAL:-0}" != "1" ]; then
  # Consumer onboarding path: do not fail Claude commands before pairing completes.
  exit 0
fi

legacy_token="$(read_file_if_empty "$legacy_token" "$legacy_token_file")"
if [ -z "$legacy_token" ]; then
  # Hooks should never block Claude execution.
  exit 0
fi

curl -fsS -m 1 -X POST \
  -H "X-AgentWatch-Token: $legacy_token" \
  "${legacy_base_url%/}/${legacy_endpoint}" >/dev/null 2>&1 &
