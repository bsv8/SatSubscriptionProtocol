#!/usr/bin/env bash
set -euo pipefail

# 仓库级三方频道验收入口；根 Go module 不会自动进入 channels 嵌套 module。
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
CHANNELS_DIR="${ROOT_DIR}/channels"

cd "${CHANNELS_DIR}"
go test ./...
go test -race ./...
go vet ./...

cd "${CHANNELS_DIR}/typescript"
npm ci --ignore-scripts --no-audit --no-fund
npm test
npm audit --omit=dev --audit-level=high
npm pack --dry-run

cd "${CHANNELS_DIR}"
./scripts/test-integration.sh
echo "channels repository acceptance passed"
