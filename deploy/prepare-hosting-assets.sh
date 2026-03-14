#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
web_root="$repo_root/deploy/web"

installer_src="$repo_root/deploy/claude-consumer-installer.sh"
installer_dst="$web_root/agentwatch.app/install/claude"

legacy_aasa_enabled="${AGENTWATCH_ENABLE_LEGACY_AASA:-0}"
pair_host_root="$web_root/pair.agentwatch.app"
aasa_well_known_path="$pair_host_root/.well-known/apple-app-site-association"
aasa_root_path="$pair_host_root/apple-app-site-association"
team_id="${AGENTWATCH_APPLE_TEAM_ID:-}"
bundle_id="${AGENTWATCH_IOS_BUNDLE_ID:-local.agentwatch.phone}"

if [ ! -f "$installer_src" ]; then
  printf '%s\n' "Installer source missing: $installer_src" >&2
  exit 66
fi

mkdir -p "$(dirname "$installer_dst")"
cp "$installer_src" "$installer_dst"
chmod 0644 "$installer_dst"

printf '%s\n' "Hosting assets generated:"
printf '%s\n' "  - $installer_dst"

if [ "$legacy_aasa_enabled" = "1" ]; then
  if [ -z "$team_id" ]; then
    printf '%s\n' "AGENTWATCH_APPLE_TEAM_ID is required when AGENTWATCH_ENABLE_LEGACY_AASA=1." >&2
    exit 64
  fi

  mkdir -p "$(dirname "$aasa_well_known_path")"

  python3 - "$team_id" "$bundle_id" "$aasa_well_known_path" "$aasa_root_path" <<'PY'
import json
import pathlib
import sys

team_id = sys.argv[1].strip()
bundle_id = sys.argv[2].strip()
well_known_path = pathlib.Path(sys.argv[3])
root_path = pathlib.Path(sys.argv[4])

app_id = f"{team_id}.{bundle_id}"

payload = {
    "applinks": {
        "details": [
            {
                "appIDs": [app_id],
                "components": [
                    {"/": "/p/*"},
                    {"/": "/p"},
                ],
            }
        ]
    }
}

encoded = json.dumps(payload, separators=(",", ":"))
well_known_path.write_text(encoded, encoding="utf-8")
root_path.write_text(encoded, encoding="utf-8")
PY

  printf '%s\n' "  - $aasa_well_known_path"
  printf '%s\n' "  - $aasa_root_path"
fi
