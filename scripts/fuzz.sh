#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOWORK=off
duration=${FUZZTIME:-3s}
go test -run='^$' -fuzz=FuzzSchemaDecode -fuzztime="$duration" -parallel=2 .
go test -run='^$' -fuzz=FuzzDispatchDenial -fuzztime="$duration" -parallel=2 .
go test -run='^$' -fuzz=FuzzStreamDecode -fuzztime="$duration" -parallel=2 ./providers/openai
