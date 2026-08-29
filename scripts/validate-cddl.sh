#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cddl_bin="$repo_dir/typescript/node_modules/.bin/cddl"

if [[ ! -x "$cddl_bin" ]]; then
  echo "缺少 CDDL 校验器，请先执行 cd typescript && npm ci" >&2
  exit 1
fi

"$cddl_bin" validate "$repo_dir/spec/v1/ssp.cddl"
