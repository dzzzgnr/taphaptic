#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

cd "$repo_root"
exec xcodebuild \
  -project Taphaptic.xcodeproj \
  -scheme TaphapticRegressionTests \
  -destination "platform=macOS" \
  CODE_SIGNING_ALLOWED=NO \
  test
