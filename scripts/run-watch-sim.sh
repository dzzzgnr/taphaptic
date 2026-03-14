#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
bundle_id="local.agentwatch.watch"
device_id="${WATCH_SIM_DEVICE_ID:-}"
paired_phone_id=""

if [ -z "$device_id" ]; then
  if pair_ids="$("$repo_root/scripts/find-sim-pair.sh" 2>/dev/null)"; then
    set -- $pair_ids
    paired_phone_id="${1:-}"
    device_id="${2:-}"
  fi
fi

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
  ls -td "$HOME"/Library/Developer/Xcode/DerivedData/*/Build/Products/Debug-watchsimulator/AgentWatch.app 2>/dev/null \
    | head -n 1
)"
if [ -z "$app_path" ]; then
  printf '%s\n' "Built app not found in DerivedData." >&2
  exit 66
fi

open -a Simulator
xcrun simctl boot "$device_id" >/dev/null 2>&1 || true
if [ -n "$paired_phone_id" ]; then
  xcrun simctl boot "$paired_phone_id" >/dev/null 2>&1 || true
fi
xcrun simctl bootstatus "$device_id" -b
if [ -n "$paired_phone_id" ]; then
  xcrun simctl bootstatus "$paired_phone_id" -b
fi
xcrun simctl uninstall "$device_id" "$bundle_id" >/dev/null 2>&1 || true
xcrun simctl install "$device_id" "$app_path"

xcrun simctl launch --terminate-running-process "$device_id" "$bundle_id"
