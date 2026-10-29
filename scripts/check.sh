#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/modules.sh"
for module in "${modules[@]}"; do
  cd "$module"
  "$root/scripts/golangci.sh" run ./...
  go vet ./...
  go build ./...
done
cd "$root"
python3 - <<'PY'
from pathlib import Path
for p in Path('.').rglob('*'):
    if p.is_file() and '.git' not in p.parts:
        data = p.read_bytes()
        if b'\0' not in data and len(data.splitlines()) > 1000:
            raise SystemExit(f'File exceeds 1,000 lines: {p}')
        if p.name == 'go.mod' and any(
            line.strip().startswith('replace') for line in data.decode().splitlines()
        ):
            raise SystemExit(f'Permanent module replacement is forbidden: {p}')
PY
