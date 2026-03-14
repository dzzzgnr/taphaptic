#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/check-hosted-installer.sh \
    [--installer-url https://codesync.me/install] \
    [--expected-api-base-url https://api.codesync.me]
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

installer_url="https://codesync.me/install"
expected_api_base_url="https://api.codesync.me"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --installer-url)
      installer_url="${2:-}"
      shift 2
      ;;
    --expected-api-base-url)
      expected_api_base_url="${2:-}"
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

tmp_home="$(mktemp -d)"
trap 'rm -rf "$tmp_home"' EXIT INT TERM

log_path="$tmp_home/install.log"

HOME="$tmp_home" sh -c "curl -fsSL \"$installer_url\" | sh" >"$log_path" 2>&1

install_root="$tmp_home/Library/Application Support/AgentWatch"
helper_path="$install_root/bin/agentwatch-hook"
api_base_path="$install_root/api-base-url"
installation_token_path="$install_root/installation-token"
installation_id_path="$install_root/installation-id"
claude_session_token_path="$install_root/claude-session-token"
settings_path="$tmp_home/.claude/settings.json"

[ -x "$helper_path" ] || { printf '%s\n' "missing helper binary: $helper_path" >&2; exit 70; }
[ -f "$api_base_path" ] || { printf '%s\n' "missing api-base-url file" >&2; exit 70; }
[ -f "$installation_token_path" ] || { printf '%s\n' "missing installation-token file" >&2; exit 70; }
[ -f "$installation_id_path" ] || { printf '%s\n' "missing installation-id file" >&2; exit 70; }
[ -f "$claude_session_token_path" ] || { printf '%s\n' "missing claude-session-token file" >&2; exit 70; }
[ -f "$settings_path" ] || { printf '%s\n' "missing Claude settings file" >&2; exit 70; }

api_base_url="$(cat "$api_base_path")"
if [ "$api_base_url" != "$expected_api_base_url" ]; then
  printf '%s\n' "api-base-url mismatch: got '$api_base_url', expected '$expected_api_base_url'" >&2
  exit 70
fi

python3 - "$settings_path" <<'PY'
import json
import sys

path = sys.argv[1]
data = json.load(open(path, "r", encoding="utf-8"))

def has_command(entries, command):
    if not isinstance(entries, list):
        return False
    for entry in entries:
        hooks = entry.get("hooks") if isinstance(entry, dict) else None
        if not isinstance(hooks, list):
            continue
        for hook in hooks:
            if isinstance(hook, dict) and hook.get("type") == "command" and hook.get("command") == command:
                return True
    return False

required = [
    ("Stop", '/bin/sh "${HOME}/Library/Application Support/AgentWatch/bin/agentwatch-hook" stop'),
    ("SubagentStop", '/bin/sh "${HOME}/Library/Application Support/AgentWatch/bin/agentwatch-hook" subagent_stop'),
    ("Notification", '/bin/sh "${HOME}/Library/Application Support/AgentWatch/bin/agentwatch-hook" permission_prompt'),
    ("Notification", '/bin/sh "${HOME}/Library/Application Support/AgentWatch/bin/agentwatch-hook" idle_prompt'),
]

missing = [(key, cmd) for key, cmd in required if not has_command(data.get(key), cmd)]
if missing:
    raise SystemExit(f"missing hook mappings: {missing}")
PY

if grep -Eq 'QR|pairing link|pairagentwatchapp|/v1/pairings|https?://.*/p/' "$log_path"; then
  printf '%s\n' "installer output still contains deprecated QR/link pairing path" >&2
  printf '%s\n' "--- installer log ---" >&2
  cat "$log_path" >&2
  exit 70
fi

if ! grep -Eq '^[[:space:]]*[0-9]{6}[[:space:]]*$' "$log_path"; then
  printf '%s\n' "installer output missing visible 6-digit pairing code" >&2
  printf '%s\n' "--- installer log ---" >&2
  cat "$log_path" >&2
  exit 70
fi

if grep -Eq 'API base URL:|Claude installation ID:|Code ID:|Code expires at:' "$log_path"; then
  printf '%s\n' "installer output exposes sensitive metadata" >&2
  printf '%s\n' "--- installer log ---" >&2
  cat "$log_path" >&2
  exit 70
fi

printf '%s\n' "Hosted installer check passed."
printf '  installer: %s\n' "$installer_url"
printf '  api base:  %s\n' "$api_base_url"
printf '  home:      %s\n' "$tmp_home"
