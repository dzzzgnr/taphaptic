#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
install_root="$HOME/Library/Application Support/AgentWatch"
install_bin_dir="$install_root/bin"
installed_binary="$install_bin_dir/agentwatch"
installed_helper="$install_bin_dir/agentwatch-hook"
token_path="$install_root/token"
launch_agents_dir="$HOME/Library/LaunchAgents"
plist_path="$launch_agents_dir/com.agentwatch.plist"
label="com.agentwatch"
uid="$(id -u)"

token="${AGENTWATCH_TOKEN:-}"
port="${PORT:-7878}"
service_name="${AGENTWATCH_SERVICE_NAME:-}"

mkdir -p "$install_bin_dir" "$launch_agents_dir"

if [ -z "$token" ] && [ -f "$token_path" ]; then
  token="$(cat "$token_path")"
fi

if [ -z "$token" ]; then
  if command -v openssl >/dev/null 2>&1; then
    token="$(openssl rand -hex 16)"
  elif command -v uuidgen >/dev/null 2>&1; then
    token="$(uuidgen | tr 'A-Z' 'a-z' | tr -d '-')"
  else
    printf '%s\n' "Unable to generate a token automatically. Set AGENTWATCH_TOKEN first." >&2
    exit 64
  fi
fi

printf '%s' "$token" > "$token_path"
chmod 600 "$token_path"

"$repo_root/scripts/build-agentwatch.sh"
cp "$repo_root/bin/agentwatch" "$installed_binary"
chmod +x "$installed_binary"
cp "$repo_root/deploy/agentwatch-hook.sh" "$installed_helper"
chmod +x "$installed_helper"

cat > "$plist_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>${label}</string>

	<key>ProgramArguments</key>
	<array>
		<string>${installed_binary}</string>
	</array>

	<key>EnvironmentVariables</key>
	<dict>
		<key>AGENTWATCH_TOKEN</key>
		<string>${token}</string>
		<key>PORT</key>
		<string>${port}</string>
		<key>AGENTWATCH_DATA_DIR</key>
		<string>${install_root}</string>
EOF

if [ -n "$service_name" ]; then
  cat >> "$plist_path" <<EOF
		<key>AGENTWATCH_SERVICE_NAME</key>
		<string>${service_name}</string>
EOF
fi

cat >> "$plist_path" <<EOF
	</dict>

	<key>RunAtLoad</key>
	<true/>

	<key>KeepAlive</key>
	<true/>

	<key>StandardOutPath</key>
	<string>${install_root}/agentwatch.log</string>

	<key>StandardErrorPath</key>
	<string>${install_root}/agentwatch.error.log</string>
</dict>
</plist>
EOF

plutil -lint "$plist_path" >/dev/null

launchctl bootout "gui/${uid}" "$plist_path" >/dev/null 2>&1 || true
launchctl bootstrap "gui/${uid}" "$plist_path"
launchctl enable "gui/${uid}/${label}"
launchctl kickstart -k "gui/${uid}/${label}"

printf '%s\n' "Installed ${label}."
printf '%s\n' "Binary: ${installed_binary}"
printf '%s\n' "Hook helper: ${installed_helper}"
printf '%s\n' "LaunchAgent: ${plist_path}"
printf '%s\n' "Token: ${token}"
