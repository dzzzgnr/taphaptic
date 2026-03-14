#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

if ! command -v xcodegen >/dev/null 2>&1; then
  printf '%s\n' "xcodegen is required. Install it with: brew install xcodegen" >&2
  exit 127
fi

if ! xcodebuild -showsdks | grep -q "watchsimulator"; then
  xcodebuild -downloadPlatform watchOS
fi

cd "$repo_root"
xcodegen generate

exec xcodebuild \
  -project AgentWatch.xcodeproj \
  -scheme AgentWatch \
  -destination "generic/platform=watchOS Simulator" \
  CODE_SIGNING_ALLOWED=NO \
  build
