#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
scope="user"
with_notifications="0"

usage() {
  printf '%s\n' "usage: sh scripts/patch-claude-settings.sh [--scope user|project] [--with-notifications]"
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
    --with-notifications)
      with_notifications="1"
      shift
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
  user)
    settings_path="$HOME/.claude/settings.json"
    ;;
  project)
    settings_path="$repo_root/.claude/settings.json"
    ;;
  *)
    printf '%s\n' "unsupported scope: $scope (use user or project)" >&2
    exit 64
    ;;
esac

if ! command -v python3 >/dev/null 2>&1; then
  printf '%s\n' "python3 is required to merge Claude settings safely." >&2
  exit 127
fi

mkdir -p "$(dirname "$settings_path")"

python3 - "$settings_path" "$with_notifications" <<'PY'
import json
import os
import sys

settings_path = sys.argv[1]
with_notifications = sys.argv[2] == "1"

STOP_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" stop'
SUBAGENT_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" subagent_stop'
PERMISSION_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" permission_prompt'
IDLE_COMMAND = '/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" idle_prompt'


def load_settings(path: str) -> dict:
    if not os.path.exists(path):
        return {}

    with open(path, "r", encoding="utf-8") as handle:
        data = json.load(handle)

    if not isinstance(data, dict):
        raise SystemExit(f"Claude settings at {path} must contain a JSON object")

    return data


def ensure_hooks_root(config: dict) -> dict:
    hooks = config.get("hooks")
    if hooks is None:
        hooks = {}
    if not isinstance(hooks, dict):
        raise SystemExit("Claude settings key 'hooks' must be an object")
    return hooks


def ensure_array(hooks: dict, key: str) -> list:
    value = hooks.get(key)
    if value is None:
        hooks[key] = []
        return hooks[key]

    if not isinstance(value, list):
        raise SystemExit(f"Claude settings key '{key}' must be an array")

    return value


def has_command(entries: list, command: str) -> bool:
    for entry in entries:
        if not isinstance(entry, dict):
            continue

        hooks = entry.get("hooks", [])
        if not isinstance(hooks, list):
            continue

        for hook in hooks:
            if isinstance(hook, dict) and hook.get("type") == "command" and hook.get("command") == command:
                return True

    return False


def add_command(entries: list, matcher: str, command: str) -> None:
    if has_command(entries, command):
        return

    entries.append(
        {
            "matcher": matcher,
            "hooks": [
                {
                    "type": "command",
                    "command": command,
                }
            ],
        }
    )


config = load_settings(settings_path)
hooks = ensure_hooks_root(config)

stop_entries = ensure_array(hooks, "Stop")
add_command(stop_entries, "*", STOP_COMMAND)

subagent_entries = ensure_array(hooks, "SubagentStop")
add_command(subagent_entries, "*", SUBAGENT_COMMAND)

if with_notifications:
    notification_entries = ensure_array(hooks, "Notification")
    add_command(notification_entries, "permission_prompt", PERMISSION_COMMAND)
    add_command(notification_entries, "idle_prompt", IDLE_COMMAND)

config["hooks"] = hooks
for legacy_key in ("Stop", "SubagentStop", "Notification"):
    if legacy_key in config:
        del config[legacy_key]

with open(settings_path, "w", encoding="utf-8") as handle:
    json.dump(config, handle, indent=2)
    handle.write("\n")
PY

printf '%s\n' "Updated Claude settings at $settings_path"
