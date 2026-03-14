#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
install_root="$HOME/Library/Application Support/Taphaptic"
api_state_root="$HOME/Library/Application Support/TaphapticAPI"
log_root="$HOME/Library/Logs/Taphaptic"
claude_settings="$HOME/.claude/settings.json"

assume_yes="0"
restore_settings="0"

usage() {
  printf '%s\n' "usage: ./scripts/uninstall.sh [--yes] [--restore-claude-settings]"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --yes)
      assume_yes="1"
      shift
      ;;
    --restore-claude-settings)
      restore_settings="1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 64
      ;;
  esac
done

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  c_reset="$(printf '\033[0m')"
  c_bold="$(printf '\033[1m')"
  c_blue="$(printf '\033[34m')"
  c_green="$(printf '\033[32m')"
  c_yellow="$(printf '\033[33m')"
else
  c_reset=""
  c_bold=""
  c_blue=""
  c_green=""
  c_yellow=""
fi

title() {
  printf '\n%s%sTaphaptic Uninstall%s\n' "$c_bold" "$c_blue" "$c_reset"
  printf '%s\n' "Removes local API/runtime files from this Mac."
}

step() {
  printf '\n%s[%s]%s %s\n' "$c_bold" "$1" "$c_reset" "$2"
}

ok() {
  printf '%s[OK]%s %s\n' "$c_green" "$c_reset" "$1"
}

warn() {
  printf '%s[WARN]%s %s\n' "$c_yellow" "$c_reset" "$1"
}

info() {
  printf '[INFO] %s\n' "$1"
}

confirm() {
  if [ "$assume_yes" = "1" ]; then
    return 0
  fi

  printf '\nProceed with uninstall? [y/N] '
  if ! IFS= read -r answer; then
    printf '\n'
    return 1
  fi

  normalized="$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')"
  case "$normalized" in
    y|yes)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

remove_dir() {
  path="$1"
  label="$2"

  if [ -e "$path" ]; then
    rm -rf "$path"
    removed_count=$((removed_count + 1))
    ok "Removed $label at $path"
  else
    info "Already missing: $label ($path)"
  fi
}

title

if ! confirm; then
  printf '%s\n' "Uninstall cancelled."
  exit 0
fi

step "1/3" "Stopping API process"
if /bin/sh "$repo_root/scripts/stop-api.sh" >/dev/null 2>&1; then
  ok "API stopped (or was already stopped)."
else
  warn "stop-api.sh returned a non-zero exit status; continuing cleanup."
fi

if command -v pkill >/dev/null 2>&1; then
  pkill -f taphaptic-api >/dev/null 2>&1 || true
fi

step "2/3" "Removing runtime files"
removed_count=0
remove_dir "$install_root" "install root"
remove_dir "$api_state_root" "API state"
remove_dir "$log_root" "log directory"

if [ "$restore_settings" = "1" ]; then
  step "3/3" "Restoring Claude settings from latest backup"
  backup_path="$(
    find "$(dirname "$claude_settings")" -maxdepth 1 -type f -name "$(basename "$claude_settings").backup.*" -print 2>/dev/null \
      | LC_ALL=C sort \
      | tail -n 1
  )"
  if [ -n "$backup_path" ]; then
    cp "$backup_path" "$claude_settings"
    ok "Restored $(basename "$claude_settings") from $(basename "$backup_path")"
  else
    warn "No Claude settings backup found. Skipped restore."
  fi
else
  step "3/3" "Claude settings"
  info "Skipped restore. Use --restore-claude-settings if you want to revert from backup."
fi

printf '\n%sUninstall complete.%s\n' "$c_bold" "$c_reset"
info "Removed paths: $removed_count"
info "Reinstall anytime with: ./scripts/bootstrap-watch.sh"

