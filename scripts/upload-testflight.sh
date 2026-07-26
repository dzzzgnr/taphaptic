#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
archive_path="${ARCHIVE_PATH:-$repo_root/build/Taphaptic.xcarchive}"
export_path="${EXPORT_PATH:-$repo_root/build/export}"

for variable_name in ASC_KEY_PATH ASC_KEY_ID ASC_ISSUER_ID; do
  eval "value=\${$variable_name:-}"
  if [ -z "$value" ]; then
    printf '%s\n' "$variable_name is required." >&2
    exit 64
  fi
done

if [ ! -d "$archive_path" ]; then
  printf '%s\n' "Archive not found: $archive_path" >&2
  exit 66
fi

mkdir -p "$export_path"
exec xcodebuild \
  -exportArchive \
  -archivePath "$archive_path" \
  -exportPath "$export_path" \
  -exportOptionsPlist "$repo_root/release/ExportOptions.plist" \
  -allowProvisioningUpdates \
  -authenticationKeyPath "$ASC_KEY_PATH" \
  -authenticationKeyID "$ASC_KEY_ID" \
  -authenticationKeyIssuerID "$ASC_ISSUER_ID"
