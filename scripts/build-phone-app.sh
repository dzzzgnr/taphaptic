#!/bin/sh

set -eu

printf '%s\n' "iPhone app build is not supported in local-only Taphaptic."
printf '%s\n' "Use ./scripts/build-watch-app.sh for the watch-only flow."
exit 64
