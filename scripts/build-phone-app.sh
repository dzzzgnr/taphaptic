#!/bin/sh

set -eu

printf '%s\n' "AgentWatchPhone is not part of the active experiment build path."
printf '%s\n' "Use ./scripts/build-watch-app.sh for the watch-only flow."
exit 64
