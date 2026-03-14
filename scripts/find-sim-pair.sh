#!/bin/sh

set -eu

unavailable=1
phone_id=""
watch_id=""
pairs_output="$(xcrun simctl list pairs)"

while IFS= read -r line; do
  case "$line" in
    [0-9A-F]*" ("*)
      unavailable=0
      case "$line" in
        *"(unavailable)"*) unavailable=1 ;;
      esac
      phone_id=""
      watch_id=""
      ;;
    *"Watch:"*)
      parsed_id="$(printf '%s\n' "$line" | sed -n 's/.*(\([A-F0-9-]\{36\}\)).*/\1/p')"
      if [ -n "$parsed_id" ]; then
        watch_id="$parsed_id"
      fi
      ;;
    *"Phone:"*)
      parsed_id="$(printf '%s\n' "$line" | sed -n 's/.*(\([A-F0-9-]\{36\}\)).*/\1/p')"
      if [ -n "$parsed_id" ]; then
        phone_id="$parsed_id"
      fi

      if [ "$unavailable" -eq 0 ] && [ -n "$phone_id" ] && [ -n "$watch_id" ]; then
        printf '%s %s\n' "$phone_id" "$watch_id"
        exit 0
      fi
      ;;
  esac
done <<EOF
$pairs_output
EOF

exit 1
