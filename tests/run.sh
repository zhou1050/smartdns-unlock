#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."

bash -n install.sh bin/smartunlock scripts/auto_unlock.sh tests/smoke_cli.sh tests/fake-bin/systemctl
python3 -m py_compile scripts/build_rules.py scripts/check_upstreams.py scripts/platform_check.py
python3 tests/test_builder.py
python3 tests/test_health.py

test_dir="$(mktemp -d)"
trap 'rm -rf -- "$test_dir"' EXIT
python3 scripts/build_rules.py --offline --output "$test_dir/generated"
jq -e '.platforms | length >= 30' "$test_dir/generated/manifest.json" >/dev/null
find "$test_dir/generated" -name '*.txt' -type f -size 0c | grep -q . && {
  echo 'empty generated rule found' >&2
  exit 1
}
echo 'offline build OK'
bash tests/smoke_cli.sh
