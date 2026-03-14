#!/bin/sh

set -eu

usage() {
  cat <<'USAGE'
usage:
  sh scripts/backup-railway-data.sh \
    [--service agentwatch-api] \
    [--environment production] \
    [--output-dir "$HOME/Library/Application Support/AgentWatch/backups"] \
    [--keep 14]
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '%s\n' "missing required command: $1" >&2
    exit 127
  fi
}

require_cmd railway
require_cmd tar
require_cmd shasum
require_cmd python3

service="agentwatch-api"
environment="production"
output_dir="$HOME/Library/Application Support/AgentWatch/backups"
keep=14

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
    --output-dir)
      output_dir="${2:-}"
      shift 2
      ;;
    --keep)
      keep="${2:-}"
      shift 2
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

case "$keep" in
  ''|*[!0-9]*)
    printf '%s\n' "--keep must be a non-negative integer, got: $keep" >&2
    exit 64
    ;;
esac

if ! railway whoami >/dev/null 2>&1; then
  printf '%s\n' "railway is not authenticated. run: railway login --browserless" >&2
  exit 69
fi

mkdir -p "$output_dir"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive_name="${service}-${environment}-${timestamp}.tar.gz"
archive_path="$output_dir/$archive_name"
tmp_path="$archive_path.tmp"
raw_capture_path="$archive_path.capture"

printf '%s\n' "Creating backup from Railway service '$service' ($environment)..."
railway ssh --service "$service" --environment "$environment" -- \
  "sh -lc 'set -eu; [ -d /data ]; printf \"__AGENTWATCH_BACKUP_BEGIN__\\n\"; cd /data; tar -czf - . | base64; printf \"\\n__AGENTWATCH_BACKUP_END__\\n\"'" > "$raw_capture_path"

python3 - "$raw_capture_path" "$tmp_path" <<'PY'
import base64
import pathlib
import sys

capture_path = pathlib.Path(sys.argv[1])
archive_path = pathlib.Path(sys.argv[2])

content = capture_path.read_bytes().decode("latin1")
begin = "__AGENTWATCH_BACKUP_BEGIN__"
end = "__AGENTWATCH_BACKUP_END__"

start = content.find(begin)
if start < 0:
    raise SystemExit("backup failed: begin marker missing")

stop = content.find(end, start + len(begin))
if stop < 0:
    raise SystemExit("backup failed: end marker missing")

payload = content[start + len(begin):stop]
allowed = set("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=\n\r")
filtered = "".join(ch for ch in payload if ch in allowed)
decoded = base64.b64decode(filtered, validate=False)

archive_path.write_bytes(decoded)
PY

rm -f "$raw_capture_path"

if [ ! -s "$tmp_path" ]; then
  rm -f "$tmp_path"
  printf '%s\n' "backup failed: archive is empty" >&2
  exit 70
fi

if ! tar -tzf "$tmp_path" >/dev/null 2>&1; then
  rm -f "$tmp_path"
  printf '%s\n' "backup failed: archive validation failed" >&2
  exit 70
fi

mv "$tmp_path" "$archive_path"

archive_sha="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
printf '%s  %s\n' "$archive_sha" "$archive_path" > "$archive_path.sha256"

python3 - "$output_dir" "$service" "$environment" "$keep" <<'PY'
import glob
import os
import sys

output_dir, service, environment, keep = sys.argv[1:5]
keep = int(keep)
if keep <= 0:
    raise SystemExit(0)

pattern = os.path.join(output_dir, f"{service}-{environment}-*.tar.gz")
paths = sorted(glob.glob(pattern), key=os.path.getmtime, reverse=True)
for stale in paths[keep:]:
    try:
        os.remove(stale)
    except FileNotFoundError:
        pass
    sha_path = stale + ".sha256"
    try:
        os.remove(sha_path)
    except FileNotFoundError:
        pass
    print(f"pruned {stale}")
PY

printf '%s\n' "Backup saved:"
printf '  %s\n' "$archive_path"
printf '  %s\n' "$archive_path.sha256"
