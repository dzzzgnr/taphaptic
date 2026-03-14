#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

if ! xcodebuild -showsdks | grep -q "watchsimulator"; then
  xcodebuild -downloadPlatform watchOS
fi

cd "$repo_root"
exec xcodebuild \
  -project Taphaptic.xcodeproj \
  -scheme Taphaptic \
  -destination "generic/platform=watchOS Simulator" \
  CODE_SIGNING_ALLOWED=NO \
  build
