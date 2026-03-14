#!/bin/sh

set -eu

event_type="${1:-}"
token="${AGENTWATCH_TOKEN:-}"
token_file="$HOME/Library/Application Support/AgentWatch/token"
installed_helper="$HOME/Library/Application Support/AgentWatch/bin/agentwatch-hook"

if [ -x "$installed_helper" ]; then
  exec /bin/sh "$installed_helper" "$event_type"
fi

case "$event_type" in
  completed)
    endpoint="complete"
    ;;
  failed)
    endpoint="failed"
    ;;
  attention)
    endpoint="attention"
    ;;
  *)
    printf '%s\n' "usage: sh hooks/agentwatch-event-hook.sh completed|failed|attention" >&2
    exit 64
    ;;
esac

if [ -z "$token" ] && [ -f "$token_file" ]; then
  token="$(cat "$token_file")"
fi

if [ -z "$token" ]; then
  printf '%s\n' "AGENTWATCH_TOKEN is not set and no token file exists at $token_file" >&2
  exit 64
fi

curl -fsS -m 1 -X POST \
  -H "X-AgentWatch-Token: $token" \
  "http://127.0.0.1:7878/$endpoint" >/dev/null 2>&1 &
