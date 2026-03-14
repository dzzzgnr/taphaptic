#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
api_path="$("$repo_root/scripts/ensure-binary.sh" taphaptic-api)"

printf '%s\n' "Prepared $api_path"
