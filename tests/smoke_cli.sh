#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "$0")/.."

test_dir="$(mktemp -d)"
trap 'rm -rf -- "$test_dir"' EXIT
mkdir -p "$test_dir/etc/rules" "$test_dir/etc/generated" "$test_dir/app/config" "$test_dir/app/scripts"
cp config/platforms.json "$test_dir/app/config/platforms.json"
cp -a rules/generated/. "$test_dir/etc/rules/"
install -m 0600 /dev/null "$test_dir/etc/upstreams.tsv"
install -m 0600 /dev/null "$test_dir/etc/enabled.tsv"
chmod +x tests/fake-bin/systemctl

run_cli() {
  PATH="$PWD/tests/fake-bin:$PATH" \
  SMARTUNLOCK_ETC_DIR="$test_dir/etc" \
  SMARTUNLOCK_APP_DIR="$test_dir/app" \
  SMARTUNLOCK_SMARTDNS_CONF="$test_dir/smartdns.conf" \
  SMARTUNLOCK_SERVICE_NAME=fake \
  bash bin/smartunlock "$@"
}

run_cli upstream-add default primary doh 'https://primary.example/dns-query'
run_cli upstream-add default backup dot 'tls://backup.example:853'
run_cli on netflix default
grep -Fq 'server-https https://primary.example/dns-query -group unlock_default -exclude-default-group' "$test_dir/etc/generated/upstreams.conf"
grep -Fq 'server-tls tls://backup.example:853 -group unlock_default -exclude-default-group -fallback' "$test_dir/etc/generated/upstreams.conf"
grep -Fq 'domain-set -name su_netflix' "$test_dir/etc/generated/platforms.conf"
grep -Fq -- '-speed-check-mode none' "$test_dir/etc/generated/platforms.conf"
printf 'default\n' > "$test_dir/etc/public-fallback.groups"
run_cli apply
! grep -Fq -- '-nameserver unlock_default' "$test_dir/etc/generated/platforms.conf"
grep -Fq '临时使用公共默认 DNS' "$test_dir/etc/generated/platforms.conf"
rm -f "$test_dir/etc/public-fallback.groups"
run_cli apply
grep -Fq -- '-nameserver unlock_default' "$test_dir/etc/generated/platforms.conf"
run_cli off netflix
! grep -Fq 'su_netflix' "$test_dir/etc/generated/platforms.conf"
echo 'CLI smoke test OK'
