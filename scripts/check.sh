#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/modules.sh"
for module in "${modules[@]}"; do
  cd "$module"
  format_diff=$(mktemp)
  if ! "$root/scripts/golangci.sh" fmt --diff ./... > "$format_diff"; then
    cat "$format_diff"
    rm -f "$format_diff"
    echo "Formatter failed; run make fmt and resolve any errors" >&2
    exit 1
  fi
  if [[ -s "$format_diff" ]]; then
    cat "$format_diff"
    rm -f "$format_diff"
    echo "Formatting differs; run make fmt" >&2
    exit 1
  fi
  rm -f "$format_diff"
  "$root/scripts/golangci.sh" run ./...
  go vet ./...
  go build -o /dev/null ./...
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
