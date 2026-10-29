#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/modules.sh"
cd "$root/examples/purchase-review"
go run .
