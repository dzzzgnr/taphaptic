#!/bin/sh

set -eu
umask 077
PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

if [ -n "${SUDO_COMMAND:-}" ] || [ "$(id -u 2>/dev/null || echo 1)" = "0" ]; then
  printf '%s\n' "Do not run this installer with sudo/root." >&2
  exit 64
fi

install_root="$HOME/Library/Application Support/Taphaptic"
install_bin_dir="$install_root/bin"
installed_helper="$install_bin_dir/taphaptic-hook"
legacy_install_root="$HOME/Library/Application Support/AgentWatch"
legacy_bin_dir="$legacy_install_root/bin"
legacy_helper="$legacy_bin_dir/agentwatch-hook"
claude_root="$HOME/.claude"

api_base_url="${TAPHAPTIC_API_BASE_URL:-${AGENTWATCH_API_BASE_URL:-http://127.0.0.1:8080}}"
api_base_url_file="$install_root/api-base-url"

installation_token_file="$install_root/installation-token"
installation_id_file="$install_root/installation-id"
claude_session_token_file="$install_root/claude-session-token"

assert_allowed_write_path() {
  path="$1"
  case "$path" in
    "$claude_root"/*|"$install_root"/*|"$legacy_install_root"/*)
      return 0
      ;;
    *)
      printf '%s\n' "Refusing to write outside allowed directories: $path" >&2
      exit 65
      ;;
  esac
}

write_file() {
  path="$1"
  value="$2"
  mode="${3:-600}"
  assert_allowed_write_path "$path"
  mkdir -p "$(dirname "$path")"
  tmp_path="$path.tmp.$$"
  printf '%s' "$value" > "$tmp_path"
  chmod "$mode" "$tmp_path"
  mv "$tmp_path" "$path"
}

save_secret_file() {
  path="$1"
  value="$2"
  write_file "$path" "$value" 600
}

read_file_if_empty() {
  current_value="$1"
  path="$2"
  if [ -z "$current_value" ] && [ -f "$path" ]; then
    cat "$path"
  else
    printf '%s' "$current_value"
  fi
}

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
if isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
' "$key"
}

api_post_json() {
  endpoint="$1"
  bearer="$2"
  payload="${3:-{}}"
  url="${api_base_url%/}/$endpoint"
  if [ -n "$bearer" ]; then
    curl -fsS --connect-timeout 1 --max-time 4 -X POST \
      -H "Authorization: Bearer $bearer" \
      -H "Content-Type: application/json" \
      -d "$payload" \
      "$url"
    return
  fi

  curl -fsS --connect-timeout 1 --max-time 4 -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$url"
}

patch_claude_settings() {
  settings_path="$claude_root/settings.json"
  assert_allowed_write_path "$settings_path"
  mkdir -p "$(dirname "$settings_path")"

  if [ -f "$settings_path" ]; then
    backup_path="$settings_path.backup.$(date -u +%Y%m%dT%H%M%SZ)"
    assert_allowed_write_path "$backup_path"
    cp "$settings_path" "$backup_path"
    chmod 600 "$backup_path"
  fi

  python3 - "$settings_path" <<'PY'
import json
import os
import sys

settings_path = sys.argv[1]

STOP_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" stop'
SUBAGENT_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" subagent_stop'
PERMISSION_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" permission_prompt'
IDLE_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" idle_prompt'

if os.path.exists(settings_path):
    with open(settings_path, "r", encoding="utf-8") as handle:
        config = json.load(handle)
        if not isinstance(config, dict):
            raise SystemExit("Claude settings must contain a JSON object.")
else:
    config = {}

hooks_config = config.get("hooks")
if not isinstance(hooks_config, dict):
    hooks_config = {}

def ensure_array(key):
    value = hooks_config.get(key)
    if value is None:
        hooks_config[key] = []
        return hooks_config[key]
    if not isinstance(value, list):
        raise SystemExit(f"Claude settings key '{key}' must be an array.")
    return value

def prune_legacy_entries(entries):
    filtered = []
    for entry in entries:
        if not isinstance(entry, dict):
            continue
        hooks = entry.get("hooks")
        if not isinstance(hooks, list):
            filtered.append(entry)
            continue

        cleaned_hooks = []
        for hook in hooks:
            if not isinstance(hook, dict):
                continue
            command = hook.get("command", "")
            if isinstance(command, str) and "/Library/Application Support/AgentWatch/" in command:
                continue
            cleaned_hooks.append(hook)

        if cleaned_hooks:
            copied = dict(entry)
            copied["hooks"] = cleaned_hooks
            filtered.append(copied)
    return filtered

def has_command(entries, command):
    for entry in entries:
        if not isinstance(entry, dict):
            continue
        hooks = entry.get("hooks")
        if not isinstance(hooks, list):
            continue
        for hook in hooks:
            if isinstance(hook, dict) and hook.get("type") == "command" and hook.get("command") == command:
                return True
    return False

def add_command(entries, matcher, command):
    if has_command(entries, command):
        return
    entries.append({
        "matcher": matcher,
        "hooks": [{"type": "command", "command": command}],
    })

stop_entries = prune_legacy_entries(ensure_array("Stop"))
add_command(stop_entries, "*", STOP_COMMAND)
hooks_config["Stop"] = stop_entries

subagent_entries = prune_legacy_entries(ensure_array("SubagentStop"))
add_command(subagent_entries, "*", SUBAGENT_COMMAND)
hooks_config["SubagentStop"] = subagent_entries

notification_entries = prune_legacy_entries(ensure_array("Notification"))
add_command(notification_entries, "permission_prompt", PERMISSION_COMMAND)
add_command(notification_entries, "idle_prompt", IDLE_COMMAND)
hooks_config["Notification"] = notification_entries

config["hooks"] = hooks_config
for legacy_key in ("Stop", "SubagentStop", "Notification"):
    if legacy_key in config:
        del config[legacy_key]

with open(settings_path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2)
    handle.write("\n")
PY
}

if ! command -v curl >/dev/null 2>&1; then
  printf '%s\n' "curl is required." >&2
  exit 127
fi
if ! command -v python3 >/dev/null 2>&1; then
  printf '%s\n' "python3 is required." >&2
  exit 127
fi

mkdir -p "$install_bin_dir"
assert_allowed_write_path "$installed_helper"
cat > "$installed_helper" <<'SH'
#!/bin/sh

set -eu
PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

action="${1:-}"
install_root="$HOME/Library/Application Support/Taphaptic"

api_base_url="${TAPHAPTIC_API_BASE_URL:-${AGENTWATCH_API_BASE_URL:-}}"
api_base_url_file="$install_root/api-base-url"

claude_session_token="${TAPHAPTIC_CLAUDE_SESSION_TOKEN:-${AGENTWATCH_CLAUDE_SESSION_TOKEN:-}}"
claude_session_token_file="$install_root/claude-session-token"
installation_token_file="$install_root/installation-token"

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

api_post_json() {
  endpoint="$1"
  bearer="$2"
  payload="${3:-{}}"
  url="${api_base_url%/}/$endpoint"
  if [ -n "$bearer" ]; then
    curl -fsS --connect-timeout 1 --max-time 3 -X POST \
      -H "Authorization: Bearer $bearer" \
      -H "Content-Type: application/json" \
      -d "$payload" \
      "$url"
    return
  fi

  curl -fsS --connect-timeout 1 --max-time 3 -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$url"
}

resolve_session_token_from_installation() {
  installation_token="$(read_file_if_empty "" "$installation_token_file")"
  if [ -z "$installation_token" ]; then
    printf '%s' ""
    return 0
  fi

  installation_response="$(api_post_json "v1/claude/installations" "$installation_token" '{}' || true)"
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
    ;;
  subagent_stop|subagent_completed)
    event_type="subagent_completed"
    title="Claude subagent completed"
    body="Claude Code subagent finished background work"
    ;;
  failed)
    event_type="failed"
    title="Claude Code failed"
    body="Claude Code reported a failure"
    ;;
  permission_prompt)
    event_type="attention"
    title="Claude Code needs permission"
    body="Claude Code is waiting for permission"
    ;;
  idle_prompt)
    event_type="attention"
    title="Claude Code is waiting"
    body="Claude Code is idle and waiting for input"
    ;;
  attention)
    event_type="attention"
    title="Claude Code needs attention"
    body="Claude Code needs attention"
    ;;
  *)
    printf '%s\n' "usage: /bin/sh taphaptic-hook stop|subagent_stop|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention" >&2
    exit 64
    ;;
esac

api_base_url="$(read_file_if_empty "$api_base_url" "$api_base_url_file")"
claude_session_token="$(read_file_if_empty "$claude_session_token" "$claude_session_token_file")"

if [ -z "$api_base_url" ]; then
  api_base_url="http://127.0.0.1:8080"
fi

if [ -z "$claude_session_token" ]; then
  claude_session_token="$(resolve_session_token_from_installation)"
fi

if [ -z "$claude_session_token" ]; then
  # Never block Claude CLI execution.
  exit 0
fi

payload="$(printf '{"type":"%s","source":"claude-code","title":"%s","body":"%s"}' "$event_type" "$title" "$body")"
api_post_json "v1/events" "$claude_session_token" "$payload" >/dev/null 2>&1 &
SH
chmod +x "$installed_helper"

mkdir -p "$legacy_bin_dir"
assert_allowed_write_path "$legacy_helper"
cat > "$legacy_helper" <<'SH'
#!/bin/sh

set -eu

exec /bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" "${1:-stop}"
SH
chmod +x "$legacy_helper"

write_file "$api_base_url_file" "$api_base_url" 600
patch_claude_settings

installation_token="$(read_file_if_empty "" "$installation_token_file")"
if [ -n "$installation_token" ]; then
  installation_response="$(api_post_json "v1/claude/installations" "$installation_token" '{}' || true)"
else
  installation_response=""
fi

if [ -z "$installation_response" ]; then
  installation_response="$(api_post_json "v1/claude/installations" "" '{}')"
fi

installation_token="$(json_field "installationToken" "$installation_response")"
installation_id="$(json_field "installationId" "$installation_response")"
claude_session_token="$(json_field "claudeSessionToken" "$installation_response")"

if [ -z "$installation_token" ] || [ -z "$installation_id" ] || [ -z "$claude_session_token" ]; then
  printf '%s\n' "Failed to create or restore local installation identity. Is the Taphaptic API running at $api_base_url?" >&2
  exit 70
fi

save_secret_file "$installation_token_file" "$installation_token"
save_secret_file "$installation_id_file" "$installation_id"
save_secret_file "$claude_session_token_file" "$claude_session_token"

watch_code_response="$(api_post_json "v1/watch/pairings/code" "$installation_token" '{}')"
watch_code="$(json_field "code" "$watch_code_response")"

if [ -z "$watch_code" ]; then
  printf '%s\n' "Failed to create watch pairing code." >&2
  exit 70
fi

printf '\n'
printf '%s\n' "$watch_code"
printf '\n'
