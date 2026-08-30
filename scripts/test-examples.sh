#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
example_dir="$repo_dir/examples/typescript"
tmp_dir="$(mktemp -d)"
had_dist=0
had_consumer_node_modules=0
had_sdk_node_modules=0
[[ -d "$repo_dir/typescript/dist" ]] && had_dist=1
[[ -d "$example_dir/node_modules" ]] && had_consumer_node_modules=1
[[ -d "$repo_dir/typescript/node_modules" ]] && had_sdk_node_modules=1
cleanup() {
  find "$tmp_dir" -depth \( -type f -o -type l \) -delete 2>/dev/null || true
  find "$tmp_dir" -depth -type d -empty -delete 2>/dev/null || true
  if [[ "$had_dist" -eq 0 ]]; then
    find "$repo_dir/typescript/dist" -depth \( -type f -o -type l \) -delete 2>/dev/null || true
    find "$repo_dir/typescript/dist" -depth -type d -empty -delete 2>/dev/null || true
  fi
  if [[ "$had_consumer_node_modules" -eq 0 ]]; then
    find "$example_dir/node_modules" -depth \( -type f -o -type l \) -delete 2>/dev/null || true
    find "$example_dir/node_modules" -depth -type d -empty -delete 2>/dev/null || true
  fi
  if [[ "$had_sdk_node_modules" -eq 0 ]]; then
    find "$repo_dir/typescript/node_modules" -depth \( -type f -o -type l \) -delete 2>/dev/null || true
    find "$repo_dir/typescript/node_modules" -depth -type d -empty -delete 2>/dev/null || true
  fi
}
trap cleanup EXIT

if [[ ! -x "$repo_dir/typescript/node_modules/.bin/tsc" ]]; then
  (cd "$repo_dir/typescript" && npm ci --ignore-scripts)
fi
(cd "$repo_dir/typescript" && npm run build)
(cd "$example_dir" && npm ci --ignore-scripts && npm start)
(cd "$repo_dir" && go run ./examples/go)
