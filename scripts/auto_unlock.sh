#!/usr/bin/env bash
set -Eeuo pipefail

ETC_DIR="${SMARTUNLOCK_ETC_DIR:-/etc/smartdns-unlock}"
APP_DIR="${SMARTUNLOCK_APP_DIR:-/opt/smartdns-unlock}"
UPSTREAM_FILE="$ETC_DIR/upstreams.tsv"
ENABLED_FILE="$ETC_DIR/enabled.tsv"
AUTO_NATIVE_FILE="$ETC_DIR/auto-native.tsv"
REPORT_FILE="$ETC_DIR/platform-check.json"
HEALTH_ENV="$ETC_DIR/health.env"
REGISTRY_FILE="$APP_DIR/config/platforms.json"
CHECKER="$APP_DIR/scripts/platform_check.py"
RRC="$APP_DIR/vendor/region-restriction-check.sh"
SMARTUNLOCK_BIN="${SMARTUNLOCK_BIN:-/usr/local/sbin/smartunlock}"
MODE="${1:-daily}"

log() { printf '[platform-check] %s\n' "$*"; }

notify_telegram() {
  local message="$1" cfg
  [[ -r "$HEALTH_ENV" ]] || return 0
  # shellcheck disable=SC1090
  source "$HEALTH_ENV"
  [[ -n "${TG_BOT_TOKEN:-}" && -n "${TG_CHAT_ID:-}" ]] || return 0
  cfg="$(mktemp "$ETC_DIR/.tg-platform.XXXXXX")"
  chmod 0600 "$cfg"
  printf 'url = "https://api.telegram.org/bot%s/sendMessage"\n' "$TG_BOT_TOKEN" > "$cfg"
  curl -fsS --retry 2 --max-time 15 --config "$cfg" \
    --data-urlencode "chat_id=$TG_CHAT_ID" \
    --data-urlencode "text=$message" >/dev/null || log "Telegram 通知发送失败"
  rm -f "$cfg"
}

platform_name() {
  jq -r --arg id "$1" '.platforms[] | select(.id == $id) | .name' "$REGISTRY_FILE" 2>/dev/null | head -1
}

status_from_report() {
  local report="$1" pid="$2"
  jq -r --arg id "$pid" '.platforms[$id].status // "unknown"' "$report" 2>/dev/null || printf 'unknown\n'
}

set_platform_group() {
  local pid="$1" group="$2" tmp
  tmp="$(mktemp "$ETC_DIR/.enabled-auto.XXXXXX")"
  awk -F'|' -v id="$pid" '$1 != id' "$ENABLED_FILE" > "$tmp"
  printf '%s|%s\n' "$pid" "$group" >> "$tmp"
  sort -u "$tmp" -o "$tmp"
  install -m 0600 "$tmp" "$ENABLED_FILE"
  rm -f "$tmp"
}

remove_native_id() {
  local pid="$1" tmp
  [[ -e "$AUTO_NATIVE_FILE" ]] || return 0
  tmp="$(mktemp "$ETC_DIR/.native-auto.XXXXXX")"
  grep -Fvx "$pid" "$AUTO_NATIVE_FILE" > "$tmp" || true
  install -m 0600 "$tmp" "$AUTO_NATIVE_FILE"
  rm -f "$tmp"
}

remove_upstream_group() {
  local group="$1" tmp
  tmp="$(mktemp "$ETC_DIR/.upstream-auto.XXXXXX")"
  awk -F'|' -v g="$group" '$1 != g' "$UPSTREAM_FILE" > "$tmp"
  install -m 0600 "$tmp" "$UPSTREAM_FILE"
  rm -f "$tmp"
}

make_backup_only_group() {
  local source_group="$1" auto_group="$2" row priority proto endpoint options tmp
  row="$(awk -F'|' -v g="$source_group" '$1 == g && $2 == "backup" {print; exit}' "$UPSTREAM_FILE")"
  [[ -n "$row" ]] || return 1
  IFS='|' read -r _ priority proto endpoint options <<<"$row"
  tmp="$(mktemp "$ETC_DIR/.upstream-auto.XXXXXX")"
  awk -F'|' -v g="$auto_group" '$1 != g' "$UPSTREAM_FILE" > "$tmp"
  printf '%s|primary|%s|%s|%s\n' "$auto_group" "$proto" "$endpoint" "${options:-}" >> "$tmp"
  install -m 0600 "$tmp" "$UPSTREAM_FILE"
  rm -f "$tmp"
}

apply_config() {
  "$SMARTUNLOCK_BIN" apply >/dev/null
}

run_full_check() {
  python3 "$CHECKER" --rrc "$RRC" --registry "$REGISTRY_FILE" --output "$REPORT_FILE" --timeout 300 >/dev/null
}

run_single_check() {
  local pid="$1" out
  out="$(mktemp "$ETC_DIR/.platform-single.XXXXXX.json")"
  python3 "$CHECKER" --rrc "$RRC" --registry "$REGISTRY_FILE" --platform "$pid" --output "$out" --timeout 60 >/dev/null || true
  status_from_report "$out" "$pid"
  rm -f "$out"
}

join_names() {
  local ids=("$@") out=() id name
  for id in "${ids[@]}"; do
    [[ -n "$id" ]] || continue
    name="$(platform_name "$id")"
    out+=("${name:-$id}")
  done
  local IFS='、'
  printf '%s' "${out[*]:-无}"
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || { log "需要 root"; exit 1; }
[[ -s "$UPSTREAM_FILE" ]] || { log "尚未配置解锁 DNS，跳过"; exit 0; }
[[ -x "$CHECKER" || -f "$CHECKER" ]] || { log "缺少平台检测器"; exit 0; }
[[ -s "$RRC" ]] || { log "缺少 RegionRestrictionCheck 探针，跳过"; notify_telegram "⚠️ SmartDNS：平台综合解锁检测未运行，缺少检测探针。"; exit 0; }

exec 9>"$ETC_DIR/.lock"
flock -w 30 9 || { log "另一个 SmartDNS Unlock 任务正在运行，本次跳过"; exit 0; }

touch "$AUTO_NATIVE_FILE"
chmod 0600 "$AUTO_NATIVE_FILE"

log "检测服务器当前综合解锁能力"
run_full_check
checked="$(jq '[.platforms[].status | select(. == "pass" or . == "fail")] | length' "$REPORT_FILE")"
if [[ "$checked" -eq 0 ]]; then
  notify_telegram "⚠️ SmartDNS：平台综合解锁检测没有取得有效结果，本次未修改配置。"
  exit 0
fi

native_fixed=()
backup_fixed=()
primary_fixed=()
unresolved=()
changed=0

# 1) Originally-native platforms: only switch to unlock DNS after a confirmed real failure.
while IFS= read -r pid; do
  [[ -n "$pid" ]] || continue
  [[ "$(status_from_report "$REPORT_FILE" "$pid")" == fail ]] || continue
  [[ "$(run_single_check "$pid")" == fail ]] || continue
  if ! awk -F'|' '$1 == "default" {found=1} END {exit !found}' "$UPSTREAM_FILE"; then
    unresolved+=("$pid")
    continue
  fi
  log "$(platform_name "$pid") 原生解锁失效，切换到 default 解锁 DNS"
  set_platform_group "$pid" default
  remove_native_id "$pid"
  apply_config
  changed=1
  if [[ "$(run_single_check "$pid")" == pass ]]; then
    native_fixed+=("$pid")
    continue
  fi
  # Main/default route is reachable but may not actually unlock this platform; try backup alone.
  auto_group="auto_${pid}"
  if make_backup_only_group default "$auto_group"; then
    set_platform_group "$pid" "$auto_group"
    apply_config
    if [[ "$(run_single_check "$pid")" == pass ]]; then
      backup_fixed+=("$pid")
      continue
    fi
    set_platform_group "$pid" default
    remove_upstream_group "$auto_group"
    apply_config
  fi
  unresolved+=("$pid")
done < "$AUTO_NATIVE_FILE"

# 2) Platforms already routed through unlock DNS: confirm failures and capability-failover default <-> backup-only.
mapfile -t failed_enabled < <(jq -r '.platforms | to_entries[] | select(.value.status == "fail") | .key' "$REPORT_FILE")
for pid in "${failed_enabled[@]}"; do
  group="$(awk -F'|' -v id="$pid" '$1 == id {print $2; exit}' "$ENABLED_FILE")"
  [[ -n "$group" ]] || continue
  [[ "$(run_single_check "$pid")" == fail ]] || continue

  if [[ "$group" == default ]]; then
    auto_group="auto_${pid}"
    if make_backup_only_group default "$auto_group"; then
      set_platform_group "$pid" "$auto_group"
      apply_config
      if [[ "$(run_single_check "$pid")" == pass ]]; then
        backup_fixed+=("$pid")
        changed=1
        continue
      fi
      set_platform_group "$pid" default
      remove_upstream_group "$auto_group"
      apply_config
    fi
    unresolved+=("$pid")
  elif [[ "$group" == auto_* ]]; then
    # Backup-only route failed: retry normal default group; if it recovered, move back.
    set_platform_group "$pid" default
    apply_config
    if [[ "$(run_single_check "$pid")" == pass ]]; then
      remove_upstream_group "$group"
      primary_fixed+=("$pid")
      changed=1
      continue
    fi
    set_platform_group "$pid" "$group"
    apply_config
    unresolved+=("$pid")
  else
    # Respect manually-created custom groups; report but do not rewrite them.
    unresolved+=("$pid")
  fi
done

# De-duplicate unresolved IDs.
if ((${#unresolved[@]})); then
  mapfile -t unresolved < <(printf '%s\n' "${unresolved[@]}" | awk 'NF && !seen[$0]++')
fi

if [[ "$changed" -eq 1 ]]; then
  log "配置发生调整，重新检测最终综合解锁能力"
  run_full_check || true
fi

pass_count="$(jq '[.platforms[].status | select(. == "pass")] | length' "$REPORT_FILE" 2>/dev/null || echo 0)"
fail_count="$(jq '[.platforms[].status | select(. == "fail")] | length' "$REPORT_FILE" 2>/dev/null || echo 0)"
unknown_count="$(jq '[.platforms[].status | select(. == "unknown")] | length' "$REPORT_FILE" 2>/dev/null || echo 0)"
host="$(hostname 2>/dev/null || printf unknown)"

prefix='✅'
((${#unresolved[@]})) && prefix='⚠️'
message="$prefix SmartDNS 综合解锁检测
主机：$host
实测平台：通过 $pass_count / 失败 $fail_count / 未判定 $unknown_count
原生失效后已切解锁：$(join_names "${native_fixed[@]:-}")
主解锁失败后备用接管：$(join_names "${backup_fixed[@]:-}")
备用失效后主线路恢复：$(join_names "${primary_fixed[@]:-}")
仍未解锁：$(join_names "${unresolved[@]:-}")"
notify_telegram "$message"
log "检测完成：通过 $pass_count，失败 $fail_count，未判定 $unknown_count"
