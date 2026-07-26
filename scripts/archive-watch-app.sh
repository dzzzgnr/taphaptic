#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
archive_path="${ARCHIVE_PATH:-$repo_root/build/Taphaptic.xcarchive}"
api_base_url="${TAPHAPTIC_API_BASE_URL:-}"
team_id="${APPLE_TEAM_ID:-X53HKYK69N}"
marketing_version="${MARKETING_VERSION:-1.0.0}"
build_number="${CURRENT_PROJECT_VERSION:-1}"

case "$api_base_url" in
  https://*)
    ;;
  *)
    printf '%s\n' "TAPHAPTIC_API_BASE_URL must be a production HTTPS URL." >&2
    exit 64
    ;;
esac

xcode_major="$(xcodebuild -version | awk 'NR == 1 { split($2, parts, "."); print parts[1] }')"
if [ -z "$xcode_major" ] || [ "$xcode_major" -lt 26 ]; then
  printf '%s\n' "Xcode 26 or later is required." >&2
  exit 1
fi

destination_report="$(
  xcodebuild \
    -project "$repo_root/Taphaptic.xcodeproj" \
    -scheme Taphaptic \
    -showdestinations 2>&1 || true
)"
if printf '%s' "$destination_report" | grep -q "watchOS .* is not installed"; then
  xcodebuild -downloadPlatform watchOS
fi

mkdir -p "$(dirname "$archive_path")"

set -- \
  -project "$repo_root/Taphaptic.xcodeproj" \
  -scheme Taphaptic \
  -configuration Release \
  -destination "generic/platform=watchOS" \
  -archivePath "$archive_path" \
  -allowProvisioningUpdates \
  DEVELOPMENT_TEAM="$team_id" \
  TAPHAPTIC_API_BASE_URL="$api_base_url" \
  MARKETING_VERSION="$marketing_version" \
  CURRENT_PROJECT_VERSION="$build_number"

if [ -n "${ASC_KEY_PATH:-}" ] && [ -n "${ASC_KEY_ID:-}" ] && [ -n "${ASC_ISSUER_ID:-}" ]; then
  set -- "$@" \
    -authenticationKeyPath "$ASC_KEY_PATH" \
    -authenticationKeyID "$ASC_KEY_ID" \
    -authenticationKeyIssuerID "$ASC_ISSUER_ID"
fi

cd "$repo_root"
exec xcodebuild "$@" archive
