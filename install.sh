#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_NAME="smartdns-unlock"
SMARTDNS_TAG="${SMARTDNS_TAG:-Release48.4}"
REPOSITORY="${SMARTUNLOCK_REPOSITORY:-zhou1050/smartdns-unlock}"
TOKEN="${GITHUB_TOKEN:-}"
APP_DIR="/opt/smartdns-unlock"
ETC_DIR="/etc/smartdns-unlock"
SMARTDNS_DIR="/etc/smartdns"
CHECK_INTERVAL="${CHECK_INTERVAL:-5m}"
HEALTH_FAIL_THRESHOLD="${HEALTH_FAIL_THRESHOLD:-3}"
HEALTH_RECOVER_THRESHOLD="${HEALTH_RECOVER_THRESHOLD:-3}"
HEALTH_COOLDOWN_SECONDS="${HEALTH_COOLDOWN_SECONDS:-300}"
TGBOT_CONFIG="${TGBOT:-}"
TG_BOT_TOKEN="${TG_BOT_TOKEN:-}"
TG_CHAT_ID="${TG_CHAT_ID:-}"
TG_REPORT_TIME="${TG_REPORT_TIME:-09:00}"
AUTO_NATIVE_DETECT="${AUTO_NATIVE_DETECT:-1}"
RRC_COMMIT="${RRC_COMMIT:-ab6829eb07c4c592c1f8f3dac736d675667d1a08}"
DNS_PREPARED=0
DNS_COMMITTED=0

info() { printf '\033[1;32m[安装]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[注意]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[失败]\033[0m %s\n' "$*" >&2; exit 1; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "请使用 sudo -E bash 运行"
[[ -r /etc/os-release ]] || die "无法识别系统"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == debian ]] || die "当前安装器仅支持 Debian"
[[ "$REPOSITORY" == */* ]] || die "请设置 SMARTUNLOCK_REPOSITORY=GitHub用户名/smartdns-unlock"
[[ "$CHECK_INTERVAL" =~ ^[1-9][0-9]*(s|m|min|h)$ ]] || die "检测周期格式错误，例如：30s、5m、10min、1h"
[[ "$HEALTH_FAIL_THRESHOLD" =~ ^[1-9][0-9]*$ ]] || die "连续失败阈值必须是正整数"
[[ "$HEALTH_RECOVER_THRESHOLD" =~ ^[1-9][0-9]*$ ]] || die "连续恢复阈值必须是正整数"
[[ "$HEALTH_COOLDOWN_SECONDS" =~ ^[0-9]+$ ]] || die "恢复冷却时间必须是秒数"
[[ "$TG_REPORT_TIME" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || die "Telegram 每日报告时间格式错误，例如：09:00"
[[ "$AUTO_NATIVE_DETECT" =~ ^[01]$ ]] || die "AUTO_NATIVE_DETECT 只能是 0 或 1"
if [[ -n "$TGBOT_CONFIG" && ( -z "$TG_BOT_TOKEN" || -z "$TG_CHAT_ID" ) ]]; then
  [[ "$TGBOT_CONFIG" == *'|'* ]] || die "TGBOT 格式应为 'BOT_TOKEN|CHAT_ID'"
  TG_BOT_TOKEN="${TGBOT_CONFIG%%|*}"
  TG_CHAT_ID="${TGBOT_CONFIG#*|}"
fi
if [[ -n "$TG_BOT_TOKEN" || -n "$TG_CHAT_ID" ]]; then
  [[ "$TG_BOT_TOKEN" =~ ^[0-9]+:[A-Za-z0-9_-]+$ ]] || die "Telegram Bot Token 格式错误"
  [[ "$TG_CHAT_ID" =~ ^(-?[0-9]+|@[A-Za-z0-9_]+)$ ]] || die "Telegram Chat ID 格式错误"
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl jq tar gzip python3 build-essential libssl-dev dnsutils util-linux >/dev/null

WORK_DIR="$(mktemp -d /tmp/smartdns-unlock-install.XXXXXX)"
cleanup() {
  local status=$?
  if [[ "$DNS_PREPARED" == 1 && "$DNS_COMMITTED" != 1 ]]; then
    restore_system_dns || true
  fi
  rm -rf -- "$WORK_DIR"
  return "$status"
}
trap cleanup EXIT

download_project() {
  info "下载仓库 $REPOSITORY"
  if [[ -n "$TOKEN" ]]; then
    printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" > "$WORK_DIR/github-curl.conf"
    chmod 0600 "$WORK_DIR/github-curl.conf"
    curl -fsSL --retry 3 --config "$WORK_DIR/github-curl.conf" \
      -H 'Accept: application/vnd.github+json' \
      "https://api.github.com/repos/$REPOSITORY/tarball/main" \
      -o "$WORK_DIR/project.tar.gz"
  else
    curl -fsSL --retry 3 \
      "https://github.com/$REPOSITORY/archive/refs/heads/main.tar.gz" \
      -o "$WORK_DIR/project.tar.gz"
  fi
  mkdir "$WORK_DIR/project"
  tar -xzf "$WORK_DIR/project.tar.gz" --strip-components=1 -C "$WORK_DIR/project"
  [[ -x "$WORK_DIR/project/bin/smartunlock" || -f "$WORK_DIR/project/bin/smartunlock" ]] || die "仓库内容不完整"
}

install_smartdns() {
  if command -v smartdns >/dev/null 2>&1 && [[ "${SMARTDNS_REINSTALL:-0}" != 1 ]]; then
    info "检测到现有 SmartDNS：$(smartdns -v 2>&1 | head -1)"
    return
  fi
  info "编译安装 SmartDNS $SMARTDNS_TAG"
  curl -fsSL --retry 3 \
    "https://github.com/pymumu/smartdns/archive/refs/tags/$SMARTDNS_TAG.tar.gz" \
    -o "$WORK_DIR/smartdns.tar.gz"
  mkdir "$WORK_DIR/smartdns-src"
  tar -xzf "$WORK_DIR/smartdns.tar.gz" --strip-components=1 -C "$WORK_DIR/smartdns-src"
  make -C "$WORK_DIR/smartdns-src" -j"$(nproc)"
  make -C "$WORK_DIR/smartdns-src" install
  command -v smartdns >/dev/null 2>&1 || die "SmartDNS 安装后未找到可执行文件"
}

install_project() {
  info "安装控制程序和规则"
  install -d -m 0755 "$APP_DIR/bin" "$APP_DIR/config" "$APP_DIR/scripts" "$APP_DIR/vendor" \
    "$ETC_DIR/rules" "$ETC_DIR/generated" "$ETC_DIR/backups" /var/cache/smartdns /var/log/smartdns
  install -m 0755 "$WORK_DIR/project/bin/smartunlock" "$APP_DIR/bin/smartunlock"
  install -m 0755 "$WORK_DIR/project/scripts/build_rules.py" "$APP_DIR/scripts/build_rules.py"
  install -m 0755 "$WORK_DIR/project/scripts/check_upstreams.py" "$APP_DIR/scripts/check_upstreams.py"
  install -m 0755 "$WORK_DIR/project/scripts/platform_check.py" "$APP_DIR/scripts/platform_check.py"
  install -m 0755 "$WORK_DIR/project/scripts/auto_unlock.sh" "$APP_DIR/scripts/auto_unlock.sh"
  install -m 0644 "$WORK_DIR/project/config/platforms.json" "$APP_DIR/config/platforms.json"
  if find "$WORK_DIR/project/rules/generated" -maxdepth 1 -name '*.txt' -type f -size +0c | grep -q .; then
    cp -a "$WORK_DIR/project/rules/generated/." "$ETC_DIR/rules/"
  else
    info "仓库尚无生成规则，使用内置种子离线生成"
    python3 "$APP_DIR/scripts/build_rules.py" --offline \
      --registry "$APP_DIR/config/platforms.json" \
      --custom-dir "$WORK_DIR/project/rules/custom" \
      --output "$ETC_DIR/rules"
  fi
  install -m 0755 "$APP_DIR/bin/smartunlock" /usr/local/sbin/smartunlock
  printf 'SMARTUNLOCK_REPOSITORY=%q\nGITHUB_TOKEN=%q\n' "$REPOSITORY" "$TOKEN" > "$ETC_DIR/github.env"
  chmod 0600 "$ETC_DIR/github.env"
  printf 'CHECK_INTERVAL=%q\nHEALTH_FAIL_THRESHOLD=%q\nHEALTH_RECOVER_THRESHOLD=%q\nHEALTH_COOLDOWN_SECONDS=%q\nTG_BOT_TOKEN=%q\nTG_CHAT_ID=%q\nTG_REPORT_TIME=%q\n' \
    "$CHECK_INTERVAL" "$HEALTH_FAIL_THRESHOLD" "$HEALTH_RECOVER_THRESHOLD" "$HEALTH_COOLDOWN_SECONDS" "$TG_BOT_TOKEN" "$TG_CHAT_ID" "$TG_REPORT_TIME" > "$ETC_DIR/health.env"
  chmod 0600 "$ETC_DIR/health.env"
  touch "$ETC_DIR/enabled.tsv" "$ETC_DIR/upstreams.tsv" "$ETC_DIR/auto-native.tsv"
  chmod 0600 "$ETC_DIR/enabled.tsv" "$ETC_DIR/upstreams.tsv" "$ETC_DIR/auto-native.tsv"
}

install_platform_probe() {
  local url="https://raw.githubusercontent.com/1-stream/RegionRestrictionCheck/$RRC_COMMIT/check.sh"
  if [[ "$AUTO_NATIVE_DETECT" != 1 ]]; then
    info "已关闭安装时原生解锁检测"
    return
  fi
  info "安装平台实际解锁探针"
  if curl -fsSL --retry 3 --max-time 30 "$url" -o "$APP_DIR/vendor/region-restriction-check.sh"; then
    chmod 0755 "$APP_DIR/vendor/region-restriction-check.sh"
  else
    rm -f "$APP_DIR/vendor/region-restriction-check.sh"
    warn "平台探针下载失败，将保守地让平台使用解锁 DNS；不影响 SmartDNS 安装"
  fi
}

detect_native_unlock() {
  local report="$ETC_DIR/native-platform-check.json" pass_count fail_count unknown_count
  : > "$ETC_DIR/auto-native.tsv"
  chmod 0600 "$ETC_DIR/auto-native.tsv"
  [[ "$AUTO_NATIVE_DETECT" == 1 ]] || return 0
  [[ -s "$APP_DIR/vendor/region-restriction-check.sh" ]] || return 0
  info "检测服务器当前原生平台解锁能力（原生可用的平台优先直连）"
  if ! python3 "$APP_DIR/scripts/platform_check.py" \
      --rrc "$APP_DIR/vendor/region-restriction-check.sh" \
      --registry "$APP_DIR/config/platforms.json" \
      --output "$report" --timeout 300 >/dev/null; then
    warn "原生平台检测失败，将保守地使用解锁 DNS"
    return 0
  fi
  jq -r '.platforms | to_entries[] | select(.value.status == "pass") | .key' "$report" > "$ETC_DIR/auto-native.tsv"
  chmod 0600 "$ETC_DIR/auto-native.tsv"
  pass_count="$(jq '[.platforms[].status | select(. == "pass")] | length' "$report")"
  fail_count="$(jq '[.platforms[].status | select(. == "fail")] | length' "$report")"
  unknown_count="$(jq '[.platforms[].status | select(. == "unknown")] | length' "$report")"
  info "原生实测：通过 $pass_count，失败 $fail_count，未判定 $unknown_count；未判定项默认走解锁 DNS"
}

write_smartdns_config() {
  info "写入 SmartDNS 配置"
  install -d -m 0755 "$SMARTDNS_DIR"
  if [[ -e "$SMARTDNS_DIR/smartdns.conf" && ! -e "$SMARTDNS_DIR/smartdns.conf.before-smartunlock" ]]; then
    cp -a "$SMARTDNS_DIR/smartdns.conf" "$SMARTDNS_DIR/smartdns.conf.before-smartunlock"
  fi
  install -m 0644 /dev/null "$SMARTDNS_DIR/smartdns.conf"
  cat > "$SMARTDNS_DIR/smartdns.conf" <<'EOF'
# Managed by smartdns-unlock. Original backup: smartdns.conf.before-smartunlock
server-name smartdns-unlock
bind 127.0.0.1:53
bind [::1]:53
cache-size 32768
cache-persist yes
cache-file /var/cache/smartdns/smartdns.cache
prefetch-domain yes
serve-expired yes
speed-check-mode tcp:443
response-mode first-ping
log-level notice
log-file /var/log/smartdns/smartdns.log
log-size 4M
log-num 3

# 普通域名：并发查询并使用 TCP 443 测速结果
server-https https://cloudflare-dns.com/dns-query -host-ip 1.1.1.1
server-https https://dns.google/dns-query -host-ip 8.8.8.8

conf-file /etc/smartdns-unlock/generated/upstreams.conf
conf-file /etc/smartdns-unlock/generated/platforms.conf
EOF
  install -m 0644 /dev/null "$ETC_DIR/generated/upstreams.conf"
  install -m 0644 /dev/null "$ETC_DIR/generated/platforms.conf"
}

detect_protocol() {
  local endpoint="$1"
  case "$endpoint" in
    https://*) printf 'doh\n' ;;
    tls://*|*:853) printf 'dot\n' ;;
    quic://*) printf 'doq\n' ;;
    h3://*) printf 'doh3\n' ;;
    tcp://*) printf 'tcp\n' ;;
    *) printf 'doh\n' ;;
  esac
}

choose_protocol() {
  local label="$1" endpoint="$2" configured="$3" detected answer
  if [[ -n "$configured" ]]; then
    answer="$configured"
  else
    detected="$(detect_protocol "$endpoint")"
    answer="$detected"
    if [[ -r /dev/tty ]]; then
      printf '%s解锁 DNS 协议 [doh/dot]（回车使用自动识别的 %s）：' "$label" "$detected" >/dev/tty
      IFS= read -r answer </dev/tty || true
      answer="${answer:-$detected}"
    fi
  fi
  [[ "$answer" =~ ^(udp|tcp|dot|doh|doq|doh3)$ ]] || die "$label解锁 DNS 协议不支持：$answer"
  printf '%s\n' "$answer"
}

configure_unlock_dns() {
  local primary="${UNLOCK_PRIMARY:-}" backup="${UNLOCK_BACKUP:-}" primary_proto="${UNLOCK_PRIMARY_PROTO:-}" backup_proto="${UNLOCK_BACKUP_PROTO:-}" id
  if [[ -z "$primary" && -r /dev/tty ]]; then
    printf '\n请输入主解锁 DNS（DoH: https://域名/dns-query；DoT: tls://域名:853；留空则稍后配置）：' >/dev/tty
    IFS= read -r primary </dev/tty || true
  fi
  if [[ -n "$primary" && -z "$backup" && -r /dev/tty ]]; then
    printf '请输入备用解锁 DNS（DoH 或 DoT，可留空）：' >/dev/tty
    IFS= read -r backup </dev/tty || true
  fi
  if [[ -z "$primary" ]]; then
    warn "未填写解锁 DNS，平台暂未启用。之后使用 smartunlock upstream-add 配置。"
    return
  fi
  primary_proto="$(choose_protocol 主 "$primary" "$primary_proto")"
  if [[ -n "$backup" ]]; then
    backup_proto="$(choose_protocol 备用 "$backup" "$backup_proto")"
  fi
  printf 'default|primary|%s|%s|\n' "$primary_proto" "$primary" > "$ETC_DIR/upstreams.tsv"
  [[ -n "$backup" ]] && printf 'default|backup|%s|%s|\n' "$backup_proto" "$backup" >> "$ETC_DIR/upstreams.tsv"

  : > "$ETC_DIR/enabled.tsv"
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    if grep -Fqx "$id" "$ETC_DIR/auto-native.tsv" 2>/dev/null; then
      continue
    fi
    printf '%s|default\n' "$id" >> "$ETC_DIR/enabled.tsv"
  done < <(jq -r '.platforms[].id' "$APP_DIR/config/platforms.json")
  chmod 0600 "$ETC_DIR/upstreams.tsv" "$ETC_DIR/enabled.tsv"
  info "原生已通过的平台保持本地直出，其余平台自动使用解锁 DNS"
}

snapshot_system_dns() {
  local backup_dir="$1"
  install -d -m 0700 "$backup_dir"
  if [[ -L /etc/resolv.conf ]]; then
    readlink /etc/resolv.conf > "$backup_dir/resolv.conf.symlink"
    cp -L /etc/resolv.conf "$backup_dir/resolv.conf" 2>/dev/null || true
  elif [[ -e /etc/resolv.conf ]]; then
    cp -a /etc/resolv.conf "$backup_dir/resolv.conf"
  else
    touch "$backup_dir/resolv.conf.missing"
  fi
  systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null && touch "$backup_dir/systemd-resolved.enabled"
  systemctl is-active --quiet systemd-resolved.service 2>/dev/null && touch "$backup_dir/systemd-resolved.active"
}

restore_system_dns() {
  local backup_dir="$WORK_DIR/system-dns-rollback"
  if [[ -f "$backup_dir/resolv.conf.symlink" ]]; then
    rm -f /etc/resolv.conf
    ln -s "$(<"$backup_dir/resolv.conf.symlink")" /etc/resolv.conf
  elif [[ -f "$backup_dir/resolv.conf" ]]; then
    rm -f /etc/resolv.conf
    install -m 0644 "$backup_dir/resolv.conf" /etc/resolv.conf
  elif [[ -f "$backup_dir/resolv.conf.missing" ]]; then
    rm -f /etc/resolv.conf
  fi
  if [[ -f "$backup_dir/systemd-resolved.enabled" ]]; then
    systemctl enable systemd-resolved.service >/dev/null 2>&1 || true
  fi
  if [[ -f "$backup_dir/systemd-resolved.active" ]]; then
    systemctl start systemd-resolved.service >/dev/null 2>&1 || true
  fi
}

prepare_system_dns() {
  local original_dir="$ETC_DIR/backups/system-dns-original"
  snapshot_system_dns "$WORK_DIR/system-dns-rollback"
  if [[ ! -d "$original_dir" ]]; then
    snapshot_system_dns "$original_dir"
  fi
  DNS_PREPARED=1
  if systemctl is-active --quiet systemd-resolved.service 2>/dev/null || systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null; then
    info "停止 systemd-resolved，释放本机 53 端口"
    systemctl disable --now systemd-resolved.service >/dev/null 2>&1 || die "无法停止 systemd-resolved"
  fi
}

configure_system_dns() {
  info "将 Debian 系统 DNS 指向本机 SmartDNS"
  rm -f /etc/resolv.conf
  cat > /etc/resolv.conf <<'EOF'
# Managed by smartdns-unlock
nameserver 127.0.0.1
nameserver ::1
options timeout:2 attempts:2
EOF
  chmod 0644 /etc/resolv.conf
  if ! dig @127.0.0.1 cloudflare.com A +time=5 +tries=2 +short | grep -q .; then
    die "本机 SmartDNS 查询失败，系统 DNS 将自动恢复"
  fi
  if ! getent ahostsv4 github.com >/dev/null 2>&1; then
    die "系统 DNS 接管验证失败，原 DNS 将自动恢复"
  fi
  DNS_COMMITTED=1
  info "系统 DNS 已接管，原配置保存在 $ETC_DIR/backups/system-dns-original"
}

install_services() {
  local smartdns_bin
  smartdns_bin="$(command -v smartdns)"
  sed "s|@SMARTDNS_BIN@|$smartdns_bin|g" "$WORK_DIR/project/systemd/smartdns-unlock.service" > /etc/systemd/system/smartdns-unlock.service
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-update.service" /etc/systemd/system/smartunlock-update.service
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-update.timer" /etc/systemd/system/smartunlock-update.timer
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-health.service" /etc/systemd/system/smartunlock-health.service
  sed "s|@CHECK_INTERVAL@|$CHECK_INTERVAL|g" "$WORK_DIR/project/systemd/smartunlock-health.timer" > /etc/systemd/system/smartunlock-health.timer
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-report.service" /etc/systemd/system/smartunlock-report.service
  sed "s|@TG_REPORT_TIME@|$TG_REPORT_TIME|g" "$WORK_DIR/project/systemd/smartunlock-report.timer" > /etc/systemd/system/smartunlock-report.timer
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-platform.service" /etc/systemd/system/smartunlock-platform.service
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-platform.timer" /etc/systemd/system/smartunlock-platform.timer
  systemctl disable --now smartdns.service 2>/dev/null || true
  systemctl daemon-reload
  systemctl enable smartdns-unlock.service smartunlock-update.timer smartunlock-health.timer smartunlock-report.timer smartunlock-platform.timer >/dev/null
  /usr/local/sbin/smartunlock apply
  systemctl start smartunlock-update.timer smartunlock-health.timer smartunlock-report.timer smartunlock-platform.timer
}

download_project
install_smartdns
install_project
install_platform_probe
detect_native_unlock
write_smartdns_config
configure_unlock_dns
prepare_system_dns
install_services
configure_system_dns
"$APP_DIR/scripts/auto_unlock.sh" install || warn "首次综合解锁复检未完成，可稍后手动执行 $APP_DIR/scripts/auto_unlock.sh"
/usr/local/sbin/smartunlock health-report

info "安装完成"
printf '\n常用命令：\n'
printf '  smartunlock list\n'
printf '  smartunlock on streaming\n'
printf '  smartunlock on ai\n'
printf '  smartunlock off netflix\n'
printf '  smartunlock status\n'
printf '  smartunlock health-check\n'
printf '  smartunlock health-report\n'
printf '  /opt/smartdns-unlock/scripts/auto_unlock.sh daily\n'
