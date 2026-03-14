#!/bin/sh

set -eu

event_type="${1:-}"
installed_helper="$HOME/Library/Application Support/Taphaptic/bin/taphaptic-hook"

if [ -x "$installed_helper" ]; then
  exec /bin/sh "$installed_helper" "$event_type"
fi

# Keep Claude execution unblocked until onboarding is completed.
exit 0
