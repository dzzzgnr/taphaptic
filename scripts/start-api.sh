#!/bin/sh

set -eu

if [ "$#" -ne 0 ]; then
  printf '%s\n' "usage: ./scripts/start-api.sh" >&2
  exit 64
fi

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
install_root="$HOME/Library/Application Support/Taphaptic"
log_dir="$HOME/Library/Logs/Taphaptic"
pid_file="$install_root/api.pid"
log_file="$log_dir/api.log"

bind_host="${TAPHAPTIC_BIND_HOST:-0.0.0.0}"
port="${TAPHAPTIC_PORT:-8080}"

probe_host="$bind_host"
case "$probe_host" in
  ""|"0.0.0.0"|"::"|"[::]"|"*")
    probe_host="127.0.0.1"
    ;;
esac

health_url="http://$probe_host:$port/healthz"
if printf '%s' "$probe_host" | grep -q ':'; then
  health_url="http://[$probe_host]:$port/healthz"
fi

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "go is required. Install Go 1.22+ from https://go.dev/dl/" >&2
  exit 127
fi

mkdir -p "$repo_root/bin"
(
  cd "$repo_root"
  go build -o "$repo_root/bin/taphapticctl" ./cmd/taphapticctl
)

health_base_url="${health_url%/healthz}"
if "$repo_root/bin/taphapticctl" health --base-url "$health_base_url" --timeout-ms 1000 >/dev/null 2>&1; then
  printf '%s\n' "API already running at http://$probe_host:$port"
  exit 0
fi

if [ -f "$pid_file" ]; then
  existing_pid="$(tr -d '[:space:]' < "$pid_file" 2>/dev/null || true)"
  if [ -n "$existing_pid" ] && kill -0 "$existing_pid" >/dev/null 2>&1; then
    existing_command="$(ps -p "$existing_pid" -o command= 2>/dev/null || true)"
    case "$existing_command" in
      *taphaptic-api*)
        printf '%s\n' "Found an existing taphaptic-api process (pid=$existing_pid) but health check failed at $health_url." >&2
        printf '%s\n' "Stop it with ./scripts/stop-api.sh and retry." >&2
        exit 69
        ;;
    esac
  fi
  rm -f "$pid_file"
fi

"$repo_root/scripts/build-taphaptic-api.sh"

mkdir -p "$install_root" "$log_dir"

"$repo_root/bin/taphaptic-api" >>"$log_file" 2>&1 &
api_pid="$!"
printf '%s\n' "$api_pid" > "$pid_file"
chmod 600 "$pid_file"

attempt=0
while [ "$attempt" -lt 25 ]; do
  if "$repo_root/bin/taphapticctl" health --base-url "$health_base_url" --timeout-ms 1000 >/dev/null 2>&1; then
    printf '%s\n' "API started (pid=$api_pid), logs: $log_file"
    exit 0
  fi

  if ! kill -0 "$api_pid" >/dev/null 2>&1; then
    break
  fi

  attempt=$((attempt + 1))
  sleep 0.2
done

printf '%s\n' "Failed to start API. Recent logs:" >&2
tail -n 40 "$log_file" >&2 || true

if kill -0 "$api_pid" >/dev/null 2>&1; then
  kill "$api_pid" >/dev/null 2>&1 || true
  wait "$api_pid" >/dev/null 2>&1 || true
fi
rm -f "$pid_file"
exit 70
