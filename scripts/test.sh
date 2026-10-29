#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/modules.sh"
for module in "${modules[@]}"; do
  cd "$module"
  go test -race "$@" ./...
done
