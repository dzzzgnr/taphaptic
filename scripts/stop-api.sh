#!/bin/sh

set -eu

if [ "$#" -ne 0 ]; then
  printf '%s\n' "usage: ./scripts/stop-api.sh" >&2
  exit 64
fi

install_root="$HOME/Library/Application Support/Taphaptic"
pid_file="$install_root/api.pid"

if [ ! -f "$pid_file" ]; then
  printf '%s\n' "API already stopped."
  exit 0
fi

api_pid="$(tr -d '[:space:]' < "$pid_file" 2>/dev/null || true)"
if [ -z "$api_pid" ]; then
  rm -f "$pid_file"
  printf '%s\n' "Removed stale PID file."
  exit 0
fi

if ! kill -0 "$api_pid" >/dev/null 2>&1; then
  rm -f "$pid_file"
  printf '%s\n' "Removed stale PID file (process not running)."
  exit 0
fi

command_line="$(ps -p "$api_pid" -o command= 2>/dev/null || true)"
case "$command_line" in
  *taphaptic-api*)
    ;;
  *)
    printf '%s\n' "Refusing to stop pid=$api_pid because it is not taphaptic-api." >&2
    exit 65
    ;;
esac

kill "$api_pid" >/dev/null 2>&1 || true

attempt=0
while [ "$attempt" -lt 25 ]; do
  if ! kill -0 "$api_pid" >/dev/null 2>&1; then
    rm -f "$pid_file"
    printf '%s\n' "API stopped."
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done

printf '%s\n' "API is still running after TERM (pid=$api_pid)." >&2
printf '%s\n' "Retry ./scripts/stop-api.sh or stop it manually." >&2
exit 70
