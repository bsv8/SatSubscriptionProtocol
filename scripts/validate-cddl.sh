#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ ! -d "$repo_dir/typescript/node_modules/cddl" ]]; then
	echo "缺少 CDDL 校验器，请先执行 cd typescript && npm ci" >&2
	exit 1
fi

(cd "$repo_dir/typescript" && node scripts/validate-cddl.mjs "$repo_dir/spec/v1/ssp.cddl")
