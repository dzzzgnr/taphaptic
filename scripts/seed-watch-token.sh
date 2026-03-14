#!/bin/sh

set -eu

bundle_id="local.agentwatch.watch"
token_path="$HOME/Library/Application Support/AgentWatch/token"
device_id="${1:-}"
token="${AGENTWATCH_TOKEN:-}"
base_url="${AGENTWATCH_BASE_URL:-http://127.0.0.1:7878}"
data_dir=""
preferences_plist=""

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

if [ -z "$token" ] && [ -f "$token_path" ]; then
  token="$(cat "$token_path")"
fi

if [ -z "$token" ]; then
  printf '%s\n' "No token available. Set AGENTWATCH_TOKEN or install the mac service first." >&2
  exit 64
fi

data_dir="$(xcrun simctl get_app_container "$device_id" "$bundle_id" data 2>/dev/null || true)"
if [ -z "$data_dir" ]; then
  printf '%s\n' "App is not installed on $device_id. Run ./scripts/run-watch-sim.sh first." >&2
  exit 66
fi

preferences_plist="$data_dir/Library/Preferences/$bundle_id.plist"
mkdir -p "$(dirname "$preferences_plist")"

if [ ! -f "$preferences_plist" ]; then
  plutil -create xml1 "$preferences_plist"
fi

/usr/libexec/PlistBuddy -c "Delete :agentwatchToken" "$preferences_plist" >/dev/null 2>&1 || true
/usr/libexec/PlistBuddy -c "Add :agentwatchToken string $token" "$preferences_plist"
/usr/libexec/PlistBuddy -c "Delete :agentwatchBaseURL" "$preferences_plist" >/dev/null 2>&1 || true
/usr/libexec/PlistBuddy -c "Add :agentwatchBaseURL string $base_url" "$preferences_plist"

printf '%s\n' "Seeded $bundle_id on $device_id"
printf '%s\n' "Token: $token"
printf '%s\n' "Base URL: $base_url"
