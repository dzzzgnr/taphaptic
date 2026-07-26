#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

destination_report="$(
  xcodebuild \
    -project "$repo_root/Taphaptic.xcodeproj" \
    -scheme Taphaptic \
    -showdestinations 2>&1 || true
)"

if printf '%s' "$destination_report" | grep -q "watchOS .* is not installed"; then
  xcodebuild -downloadPlatform watchOS
fi

cd "$repo_root"
exec xcodebuild \
  -project Taphaptic.xcodeproj \
  -scheme Taphaptic \
  -destination "generic/platform=watchOS Simulator" \
  build
