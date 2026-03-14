#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

mkdir -p "$repo_root/bin"
go build -o "$repo_root/bin/agentwatch-api" ./cmd/agentwatch-api

printf '%s\n' "Built $repo_root/bin/agentwatch-api"
