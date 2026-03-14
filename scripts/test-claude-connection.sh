#!/bin/sh

set -eu

helper_path="$HOME/Library/Application Support/AgentWatch/bin/agentwatch-hook"
action="${1:-stop}"

if [ ! -x "$helper_path" ]; then
  printf '%s\n' "AgentWatch hook helper is not installed yet. Run ./scripts/install-claude-hook.sh first." >&2
  exit 69
fi

exec /bin/sh "$helper_path" "$action"
