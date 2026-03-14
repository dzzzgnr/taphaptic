#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
bundle_id="local.taphaptic.watch"
device_id="${WATCH_SIM_DEVICE_ID:-}"

if [ -z "$device_id" ]; then
  device_id="$(xcrun simctl list devices available | awk '
    /^-- watchOS / { in_watch = 1; next }
    /^-- / { in_watch = 0 }
    in_watch && /Apple Watch/ { print; exit }
  ' | sed -n 's/.*(\([A-F0-9-]\{36\}\)).*/\1/p')"
fi

if [ -z "$device_id" ]; then
  printf '%s\n' "No available watch simulator found." >&2
  exit 69
fi

"$repo_root/scripts/build-watch-app.sh"

app_path="$(
  ls -td "$HOME"/Library/Developer/Xcode/DerivedData/*/Build/Products/Debug-watchsimulator/Taphaptic.app 2>/dev/null \
    | head -n 1
)"
if [ -z "$app_path" ]; then
  printf '%s\n' "Built app not found in DerivedData." >&2
  exit 66
fi

open -a Simulator
xcrun simctl boot "$device_id" >/dev/null 2>&1 || true
xcrun simctl bootstatus "$device_id" -b
xcrun simctl uninstall "$device_id" "$bundle_id" >/dev/null 2>&1 || true
xcrun simctl install "$device_id" "$app_path"

xcrun simctl launch --terminate-running-process "$device_id" "$bundle_id"
