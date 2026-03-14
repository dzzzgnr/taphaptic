#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
bundle_id="local.agentwatch.watch"

"$repo_root/scripts/build-watch-app.sh"

app_path="$(
  ls -td "$HOME"/Library/Developer/Xcode/DerivedData/*/Build/Products/Debug-watchsimulator/AgentWatch.app 2>/dev/null \
    | head -n 1
)"
if [ -z "$app_path" ]; then
  printf '%s\n' "Built app not found in DerivedData." >&2
  exit 66
fi

device_ids="$(
  xcrun simctl list devices booted available | awk '
    /^-- watchOS / { in_watch = 1; next }
    /^-- / { in_watch = 0 }
    in_watch && /Apple Watch/ {
      if (match($0, /\(([A-F0-9-]{36})\)/)) {
        print substr($0, RSTART + 1, RLENGTH - 2)
      }
    }
  '
)"

if [ -z "$device_ids" ]; then
  printf '%s\n' "No booted watch simulators found." >&2
  exit 69
fi

open -a Simulator

printf '%s\n' "$device_ids" | while IFS= read -r device_id; do
  [ -n "$device_id" ] || continue
  printf 'Updating %s\n' "$device_id"
  (
    xcrun simctl uninstall "$device_id" "$bundle_id" >/dev/null 2>&1 || true
    xcrun simctl install "$device_id" "$app_path"
    xcrun simctl launch --terminate-running-process "$device_id" "$bundle_id" >/dev/null
  ) &
done

wait

printf '%s\n' "Updated AgentWatch on all booted watch simulators."
