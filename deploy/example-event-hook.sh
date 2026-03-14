#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
exec sh "$repo_root/hooks/agentwatch-event-hook.sh" "${1:-}"
