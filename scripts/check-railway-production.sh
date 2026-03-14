#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/check-railway-production.sh \
    [--service agentwatch-api] \
    [--environment production] \
    [--log-lines 200] \
    [--require-apns] \
    [--strict]
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "missing required command: $1" >&2
    exit 127
  fi
}

require_cmd railway
require_cmd curl
require_cmd python3

service="agentwatch-api"
environment="production"
log_lines=200
strict=0
require_apns=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --service)
      service="${2:-}"
      shift 2
      ;;
    --environment)
      environment="${2:-}"
      shift 2
      ;;
    --log-lines)
      log_lines="${2:-}"
      shift 2
      ;;
    --strict)
      strict=1
      shift 1
      ;;
    --require-apns)
      require_apns=1
      shift 1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '%s\n' "unknown argument: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

case "$log_lines" in
  ''|*[!0-9]*)
    printf '%s\n' "--log-lines must be a positive integer, got: $log_lines" >&2
    exit 64
    ;;
esac

if ! railway whoami >/dev/null 2>&1; then
  printf '%s\n' "railway is not authenticated. run: railway login --browserless" >&2
  exit 69
fi

status_json="$(railway service status --service "$service" --environment "$environment" --json)"
deployment_status="$(printf '%s' "$status_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))')"

if [ "$deployment_status" != "SUCCESS" ]; then
  printf '%s\n' "FAIL: deployment status is '$deployment_status' (expected SUCCESS)" >&2
  exit 70
fi

vars_kv="$(railway variable list --service "$service" --environment "$environment" --kv || true)"
api_base_url="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_PUBLIC_API_BASE_URL"{print substr($0,index($0,"=")+1)}' | head -n1)"

if [ -z "$api_base_url" ]; then
  domain_json="$(railway domain -s "$service" --json)"
  api_base_url="$(printf '%s' "$domain_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); domains=d.get("domains") or []; domain=d.get("domain"); print((domains[0] if domains else domain) or "")')"
fi

if [ -z "$api_base_url" ]; then
  printf '%s\n' "FAIL: could not resolve API base URL from Railway config" >&2
  exit 70
fi

health_code_and_time="$(curl -fsS -o /dev/null -w '%{http_code} %{time_total}' "${api_base_url%/}/healthz" || true)"
health_code="$(printf '%s' "$health_code_and_time" | awk '{print $1}')"
health_time="$(printf '%s' "$health_code_and_time" | awk '{print $2}')"

if [ "$health_code" != "204" ]; then
  printf '%s\n' "FAIL: /healthz returned HTTP $health_code" >&2
  exit 70
fi

install_json="$(curl -fsS -X POST -H 'Content-Type: application/json' -d '{}' "${api_base_url%/}/v1/claude/installations" || true)"
install_id="$(printf '%s' "$install_json" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("installationId",""))' 2>/dev/null || true)"
if [ -z "$install_id" ]; then
  printf '%s\n' "FAIL: installation bootstrap endpoint check failed" >&2
  exit 70
fi

logs="$(railway logs --service "$service" --environment "$environment" --lines "$log_lines" 2>/dev/null || true)"

errors_found="$(printf '%s\n' "$logs" | python3 -c '
import re,sys
text=sys.stdin.read()
patterns=[r"panic",r"fatal",r"api\\.exited_with_error",r"push_failed",r"\\berror="]
count=0
for line in text.splitlines():
    l=line.lower()
    if any(re.search(p,l) for p in patterns):
        count+=1
print(count)
')"

apns_team="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_APNS_TEAM_ID"{print substr($0,index($0,"=")+1)}' | head -n1)"
apns_key_id="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_APNS_KEY_ID"{print substr($0,index($0,"=")+1)}' | head -n1)"
apns_topic="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_APNS_TOPIC"{print substr($0,index($0,"=")+1)}' | head -n1)"
apns_key_path="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_APNS_PRIVATE_KEY_PATH"{print substr($0,index($0,"=")+1)}' | head -n1)"
apns_sandbox="$(printf '%s\n' "$vars_kv" | awk -F= '$1=="AGENTWATCH_APNS_SANDBOX"{print substr($0,index($0,"=")+1)}' | head -n1)"

push_configured="yes"
if [ -z "$apns_team" ] || [ -z "$apns_key_id" ] || [ -z "$apns_topic" ] || [ -z "$apns_key_path" ]; then
  push_configured="no"
fi

warnings=0
if [ "$errors_found" -gt 0 ]; then
  warnings=$((warnings + 1))
fi
if [ "$require_apns" -eq 1 ] && [ "$push_configured" != "yes" ]; then
  warnings=$((warnings + 1))
fi

printf '%s\n' "Railway production check:"
printf '  service: %s (%s)\n' "$service" "$environment"
printf '  deployment: %s\n' "$deployment_status"
printf '  api: %s\n' "$api_base_url"
printf '  healthz: HTTP %s in %ss\n' "$health_code" "$health_time"
printf '  bootstrap endpoint: ok (%s)\n' "$install_id"
printf '  log issues (last %s lines): %s\n' "$log_lines" "$errors_found"
printf '  APNs configured: %s\n' "$push_configured"
if [ "$push_configured" = "yes" ]; then
  printf '  APNs topic: %s (sandbox=%s)\n' "$apns_topic" "${apns_sandbox:-false}"
elif [ "$require_apns" -eq 0 ]; then
  printf '%s\n' "  APNs check: skipped (watch-only MVP mode)"
fi

if [ "$strict" -eq 1 ] && [ "$warnings" -gt 0 ]; then
  printf '%s\n' "FAIL: strict mode detected $warnings warning(s)." >&2
  exit 72
fi

exit 0
