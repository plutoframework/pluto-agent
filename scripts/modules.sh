#!/usr/bin/env bash
# Sourced by development checks. The temporary workspace never changes pins.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
modules=("$root" "$root/integrations/plutorbac" "$root/integrations/otel" "$root/examples/purchase-review")
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
export GOWORK="$workspace/go.work"
go work init "${modules[@]}"
# Go may need unreleased nested-module go.mod files while resolving ambiguous
# package prefixes. These workspace-only replacements disappear on exit.
go work edit -replace="github.com/plutoframework/pluto-agent@v0.1.0=$root"
go work edit -replace="github.com/plutoframework/pluto-agent/integrations/plutorbac@v0.1.0=$root/integrations/plutorbac"
