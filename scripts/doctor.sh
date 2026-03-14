#!/bin/sh

set -eu

if [ "$#" -ne 0 ]; then
  printf '%s\n' "usage: ./scripts/doctor.sh" >&2
  exit 64
fi

missing=0

check_command() {
  command_name="$1"
  install_hint="$2"
  if command -v "$command_name" >/dev/null 2>&1; then
    return 0
  fi

  printf '%s\n' "Missing required command: $command_name" >&2
  if [ -n "$install_hint" ]; then
    printf '%s\n' "Install hint: $install_hint" >&2
  fi
  missing=1
}

check_command "xcodebuild" "Install Xcode from the App Store."

if ! command -v curl >/dev/null 2>&1 && ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "Missing required runtime helper: install either curl (for prebuilt binary downloads) or Go (developer mode source builds)." >&2
  missing=1
fi

if ! command -v xcode-select >/dev/null 2>&1; then
  printf '%s\n' "Missing required command: xcode-select" >&2
  printf '%s\n' "Install Xcode Command Line Tools: xcode-select --install" >&2
  missing=1
elif ! xcode_path="$(xcode-select -p 2>/dev/null)"; then
  printf '%s\n' "xcode-select is installed but has no active developer directory." >&2
  printf '%s\n' "Run: sudo xcode-select -s /Applications/Xcode.app/Contents/Developer" >&2
  missing=1
elif [ ! -d "$xcode_path" ]; then
  printf '%s\n' "xcode-select path does not exist: $xcode_path" >&2
  printf '%s\n' "Run: sudo xcode-select -s /Applications/Xcode.app/Contents/Developer" >&2
  missing=1
fi

if [ "$missing" -ne 0 ]; then
  printf '\n%s\n' "Preflight failed. Fix the missing requirements above and re-run ./scripts/bootstrap-watch.sh." >&2
  exit 1
fi

printf '%s\n' "Preflight passed."
