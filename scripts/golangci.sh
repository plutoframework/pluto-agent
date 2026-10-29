#!/usr/bin/env bash
# One pinned version for mise tasks, direct scripts, and CI.
set -euo pipefail
tool_root=$(cd "$(dirname "$0")/.." && pwd)

version=$(python3 - "$tool_root/mise.toml" <<'PYTHON'
import tomllib
import sys
from pathlib import Path
print(tomllib.loads(Path(sys.argv[1]).read_text())['tools']['golangci-lint'])
PYTHON
)

if command -v golangci-lint >/dev/null 2>&1; then
  installed=$(golangci-lint version --short)
  if [[ "$installed" == "$version" ]]; then
    exec golangci-lint "$@"
  fi
fi

if command -v mise >/dev/null 2>&1; then
  exec mise exec "golangci-lint@$version" -- golangci-lint "$@"
fi

echo "Install golangci-lint $version or run 'mise install' in this project." >&2
exit 1
