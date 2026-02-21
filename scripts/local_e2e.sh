#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p ./bin
go build -o ./bin/wtx .

E2E_HOME="$(mktemp -d)"
trap 'rm -rf "$E2E_HOME"' EXIT

HOME="$E2E_HOME" \
WTX_E2E_BIN="$ROOT_DIR/bin/wtx" \
WTX_LOCAL_E2E=1 \
go test -tags local_e2e ./e2e -count=1 "$@"
