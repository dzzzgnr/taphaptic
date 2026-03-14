#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

exec /bin/sh "$repo_root/deploy/claude-consumer-installer.sh"
