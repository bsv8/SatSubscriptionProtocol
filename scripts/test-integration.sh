#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
had_dist=0
had_node_modules=0
[[ -d "$repo_dir/typescript/dist" ]] && had_dist=1
[[ -d "$repo_dir/typescript/node_modules" ]] && had_node_modules=1
cleanup() {
  rm -rf "$tmp_dir"
  if [[ "$had_dist" -eq 0 ]]; then
    rm -rf "$repo_dir/typescript/dist"
  fi
  if [[ "$had_node_modules" -eq 0 ]]; then
    rm -rf "$repo_dir/typescript/node_modules"
  fi
}
trap cleanup EXIT

compare_json_files() {
  local left="$1"
  local right="$2"
  if ! cmp -s "$left" "$right"; then
    echo "跨语言集成结果不一致: $left vs $right" >&2
    exit 1
  fi
}

if [[ ! -x "$repo_dir/typescript/node_modules/.bin/tsc" || ! -x "$repo_dir/typescript/node_modules/.bin/cddl" ]]; then
  (cd "$repo_dir/typescript" && npm ci --ignore-scripts)
fi
(cd "$repo_dir" && ./scripts/validate-cddl.sh)
(cd "$repo_dir/typescript" && npm run build)

go_encode="$tmp_dir/go-encode.json"
ts_encode="$tmp_dir/ts-encode.json"
ts_from_go="$tmp_dir/ts-from-go.json"
go_from_ts="$tmp_dir/go-from-ts.json"

(cd "$repo_dir" && go run ./integration/fixture -mode encode) > "$go_encode"
(cd "$repo_dir" && node typescript/test/integration-cli.js encode) > "$ts_encode"
compare_json_files "$go_encode" "$ts_encode"

(cd "$repo_dir" && node typescript/test/integration-cli.js decode "$go_encode") > "$ts_from_go"
(cd "$repo_dir" && go run ./integration/fixture -mode decode -input "$ts_encode") > "$go_from_ts"
compare_json_files "$go_encode" "$ts_from_go"
compare_json_files "$ts_encode" "$go_from_ts"

go_state="$tmp_dir/go-state.json"
ts_state="$tmp_dir/ts-state.json"
(cd "$repo_dir" && go run ./integration/fixture -mode state) > "$go_state"
(cd "$repo_dir" && node typescript/test/integration-cli.js state) > "$ts_state"
compare_json_files "$go_state" "$ts_state"

go_invalid="$tmp_dir/go-invalid.json"
ts_invalid="$tmp_dir/ts-invalid.json"
(cd "$repo_dir" && go run ./integration/fixture -mode invalid) > "$go_invalid"
(cd "$repo_dir" && node typescript/test/integration-cli.js invalid) > "$ts_invalid"
compare_json_files "$go_invalid" "$ts_invalid"

go_service="$tmp_dir/go-service.json"
ts_service="$tmp_dir/ts-service.json"
(cd "$repo_dir" && go run ./integration/fixture -mode service) > "$go_service"
(cd "$repo_dir" && node typescript/test/integration-cli.js service) > "$ts_service"
compare_json_files "$go_service" "$ts_service"

go_dispatch="$tmp_dir/go-dispatch.json"
ts_dispatch="$tmp_dir/ts-dispatch.json"
(cd "$repo_dir" && go run ./integration/fixture -mode dispatch) > "$go_dispatch"
(cd "$repo_dir" && node typescript/test/integration-cli.js dispatch) > "$ts_dispatch"
compare_json_files "$go_dispatch" "$ts_dispatch"

go_billing="$tmp_dir/go-billing.json"
ts_billing="$tmp_dir/ts-billing.json"
(cd "$repo_dir" && go run ./integration/fixture -mode billing) > "$go_billing"
(cd "$repo_dir" && node typescript/test/integration-cli.js billing) > "$ts_billing"
compare_json_files "$go_billing" "$ts_billing"

go_billing_query="$tmp_dir/go-billing-query.json"
ts_billing_query="$tmp_dir/ts-billing-query.json"
(cd "$repo_dir" && go run ./integration/fixture -mode billing-query) > "$go_billing_query"
(cd "$repo_dir" && node typescript/test/integration-cli.js billing-query) > "$ts_billing_query"
compare_json_files "$go_billing_query" "$ts_billing_query"

"$repo_dir/scripts/test-examples.sh"

echo "SSP Go ↔ TypeScript 纯核心集成通过"
