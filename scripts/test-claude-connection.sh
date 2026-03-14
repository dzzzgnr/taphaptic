#!/bin/sh

set -eu

helper_path="$HOME/Library/Application Support/Taphaptic/bin/taphaptic-hook"
action="${1:-stop}"

if [ ! -x "$helper_path" ]; then
  printf '%s\n' "Taphaptic hook helper is not installed yet. Run ./scripts/install-claude-hook.sh first." >&2
  exit 69
fi

exec /bin/sh "$helper_path" "$action"
