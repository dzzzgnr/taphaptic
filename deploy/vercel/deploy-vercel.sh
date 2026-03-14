#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
web_root="$repo_root/deploy/web"
legacy_aasa_enabled="${AGENTWATCH_ENABLE_LEGACY_AASA:-0}"

usage() {
  printf '%s\n' "usage: [AGENTWATCH_ENABLE_LEGACY_AASA=1 AGENTWATCH_APPLE_TEAM_ID=<team> AGENTWATCH_IOS_BUNDLE_ID=<bundle>] sh deploy/vercel/deploy-vercel.sh"
}

if [ "$legacy_aasa_enabled" = "1" ] && [ -z "${AGENTWATCH_APPLE_TEAM_ID:-}" ]; then
  usage >&2
  exit 64
fi

if ! command -v vercel >/dev/null 2>&1; then
  printf '%s\n' "vercel CLI is required. Install with: npm i -g vercel" >&2
  exit 127
fi

cd "$repo_root"
./deploy/prepare-hosting-assets.sh

printf '%s\n' "Deploying agentwatch.app static project..."
cd "$web_root/agentwatch.app"
vercel deploy --prod --yes

if [ "$legacy_aasa_enabled" = "1" ] && [ -d "$web_root/pair.agentwatch.app" ]; then
  printf '%s\n' "Deploying pair.agentwatch.app static project (legacy)..."
  cd "$web_root/pair.agentwatch.app"
  vercel deploy --prod --yes
fi

printf '%s\n' ""
printf '%s\n' 'Next: attach domains in Vercel dashboard (or `vercel domains add`):'
printf '%s\n' "  - agentwatch.app -> deploy/web/agentwatch.app project"
if [ "$legacy_aasa_enabled" = "1" ]; then
  printf '%s\n' "  - pair.agentwatch.app -> deploy/web/pair.agentwatch.app project (legacy)"
fi
