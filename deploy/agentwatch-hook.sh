#!/bin/sh

set -eu

installed_helper="$HOME/Library/Application Support/Taphaptic/bin/taphaptic-hook"

if [ -x "$installed_helper" ]; then
  exec /bin/sh "$installed_helper" "$@"
fi

# No-op fallback so Claude hooks do not break before setup.
exit 0
