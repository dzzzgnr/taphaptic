#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
  printf '%s\n' "usage: ./scripts/ensure-binary.sh <taphapticctl|taphaptic-api>" >&2
  exit 64
fi

tool="$1"
repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
bin_dir="$repo_root/bin"
mkdir -p "$bin_dir"

release_repo="${TAPHAPTIC_RELEASE_REPO:-dzzzgnr/taphaptic}"
release_tag="${TAPHAPTIC_RELEASE_TAG:-}"
dev_mode="${TAPHAPTIC_DEV_MODE:-0}"

case "$tool" in
  taphapticctl)
    destination="$bin_dir/taphapticctl"
    source_pkg="./cmd/taphapticctl"
    path_override_key="TAPHAPTICCTL_PATH"
    path_override="${TAPHAPTICCTL_PATH:-}"
    download_override_key="TAPHAPTICCTL_DOWNLOAD_URL"
    download_override="${TAPHAPTICCTL_DOWNLOAD_URL:-}"
    ;;
  taphaptic-api)
    destination="$bin_dir/taphaptic-api"
    source_pkg="./cmd/taphaptic-api"
    path_override_key="TAPHAPTIC_API_PATH"
    path_override="${TAPHAPTIC_API_PATH:-}"
    download_override_key="TAPHAPTIC_API_DOWNLOAD_URL"
    download_override="${TAPHAPTIC_API_DOWNLOAD_URL:-}"
    ;;
  *)
    printf '%s\n' "unsupported tool: $tool (expected taphapticctl or taphaptic-api)" >&2
    exit 64
    ;;
esac

if [ "$dev_mode" = "1" ]; then
  if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' "Developer mode enabled, but go is not installed. Install Go 1.22+ from https://go.dev/dl/" >&2
    exit 127
  fi
  (
    cd "$repo_root"
    go build -o "$destination" "$source_pkg"
  )
  chmod 755 "$destination"
  printf '%s\n' "Built $tool from source (developer mode)." >&2
  printf '%s\n' "$destination"
  exit 0
fi

if [ -x "$destination" ]; then
  printf '%s\n' "$destination"
  exit 0
fi

if [ -n "$path_override" ]; then
  if [ ! -x "$path_override" ]; then
    printf '%s\n' "$path_override_key is set but not executable: $path_override" >&2
    exit 66
  fi
  cp "$path_override" "$destination"
  chmod 755 "$destination"
  printf '%s\n' "Prepared $tool from $path_override." >&2
  printf '%s\n' "$destination"
  exit 0
fi

platform_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$platform_os" in
  darwin|linux)
    ;;
  *)
    platform_os="unsupported"
    ;;
esac

machine_arch="$(uname -m)"
case "$machine_arch" in
  x86_64|amd64)
    platform_arch="amd64"
    ;;
  arm64|aarch64)
    platform_arch="arm64"
    ;;
  *)
    platform_arch="unsupported"
    ;;
esac

asset_name="${tool}_${platform_os}_${platform_arch}"
download_url=""

if [ -n "$download_override" ]; then
  download_url="$download_override"
elif [ "$platform_os" != "unsupported" ] && [ "$platform_arch" != "unsupported" ]; then
  if [ -n "$release_tag" ]; then
    download_url="https://github.com/$release_repo/releases/download/$release_tag/$asset_name"
  else
    download_url="https://github.com/$release_repo/releases/latest/download/$asset_name"
  fi
fi

download_failed=0
if [ -n "$download_url" ]; then
  if command -v curl >/dev/null 2>&1; then
    tmp_file="$(mktemp "${TMPDIR:-/tmp}/${tool}.XXXXXX")"
    trap 'rm -f "$tmp_file"' EXIT HUP INT TERM
    if curl -fLSs --retry 2 --connect-timeout 5 -o "$tmp_file" "$download_url"; then
      chmod 755 "$tmp_file"
      mv "$tmp_file" "$destination"
      trap - EXIT HUP INT TERM
      printf '%s\n' "Prepared $tool from prebuilt release asset ($download_url)." >&2
      printf '%s\n' "$destination"
      exit 0
    fi
    download_failed=1
    rm -f "$tmp_file"
    trap - EXIT HUP INT TERM
  else
    download_failed=1
    printf '%s\n' "curl is required to download prebuilt binaries automatically." >&2
  fi
fi

printf '%s\n' "Unable to prepare $tool binary." >&2
printf '%s\n' "Checked existing path: $destination" >&2
if [ -n "$download_url" ]; then
  if [ "$download_failed" -eq 1 ]; then
    printf '%s\n' "Download failed from: $download_url" >&2
  fi
else
  printf '%s\n' "No prebuilt asset mapping for this platform ($platform_os/$platform_arch)." >&2
fi
printf '%s\n' "Set $path_override_key to an executable path, set $download_override_key to a direct binary URL, or enable source builds with TAPHAPTIC_DEV_MODE=1 (requires Go)." >&2
exit 127
