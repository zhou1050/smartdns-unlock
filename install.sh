#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_NAME="smartdns-unlock"
SMARTDNS_TAG="${SMARTDNS_TAG:-Release48.4}"
REPOSITORY="${SMARTUNLOCK_REPOSITORY:-}"
TOKEN="${GITHUB_TOKEN:-}"
APP_DIR="/opt/smartdns-unlock"
ETC_DIR="/etc/smartdns-unlock"
SMARTDNS_DIR="/etc/smartdns"

info() { printf '\033[1;32m[安装]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[注意]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[失败]\033[0m %s\n' "$*" >&2; exit 1; }

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "请使用 sudo -E bash 运行"
[[ -r /etc/os-release ]] || die "无法识别系统"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == debian ]] || die "当前安装器仅支持 Debian"
[[ "$REPOSITORY" == */* ]] || die "请设置 SMARTUNLOCK_REPOSITORY=GitHub用户名/smartdns-unlock"
[[ -n "$TOKEN" ]] || die "私有仓库安装必须设置只读 GITHUB_TOKEN"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl jq tar gzip python3 build-essential libssl-dev dnsutils util-linux >/dev/null

WORK_DIR="$(mktemp -d /tmp/smartdns-unlock-install.XXXXXX)"
cleanup() { rm -rf -- "$WORK_DIR"; }
trap cleanup EXIT

download_project() {
  info "下载私有仓库 $REPOSITORY"
  printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" > "$WORK_DIR/github-curl.conf"
  chmod 0600 "$WORK_DIR/github-curl.conf"
  curl -fsSL --retry 3 --config "$WORK_DIR/github-curl.conf" \
    -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/$REPOSITORY/tarball/main" \
    -o "$WORK_DIR/project.tar.gz"
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
  install -d -m 0755 "$APP_DIR/bin" "$APP_DIR/config" "$APP_DIR/scripts" "$ETC_DIR/rules" "$ETC_DIR/generated" "$ETC_DIR/backups" /var/cache/smartdns /var/log/smartdns
  install -m 0755 "$WORK_DIR/project/bin/smartunlock" "$APP_DIR/bin/smartunlock"
  install -m 0755 "$WORK_DIR/project/scripts/build_rules.py" "$APP_DIR/scripts/build_rules.py"
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
  touch "$ETC_DIR/enabled.tsv" "$ETC_DIR/upstreams.tsv"
  chmod 0600 "$ETC_DIR/enabled.tsv" "$ETC_DIR/upstreams.tsv"
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
bind [::]:53
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

configure_unlock_dns() {
  local primary="${UNLOCK_PRIMARY:-}" backup="${UNLOCK_BACKUP:-}" primary_proto="${UNLOCK_PRIMARY_PROTO:-doh}" backup_proto="${UNLOCK_BACKUP_PROTO:-dot}"
  if [[ -z "$primary" && -r /dev/tty ]]; then
    printf '\n请输入主解锁 DNS（DoH URL、DoT 主机或 DNS IP；留空则稍后配置）：' >/dev/tty
    IFS= read -r primary </dev/tty || true
  fi
  if [[ -n "$primary" && -z "$backup" && -r /dev/tty ]]; then
    printf '请输入备用解锁 DNS（可留空）：' >/dev/tty
    IFS= read -r backup </dev/tty || true
  fi
  if [[ -z "$primary" ]]; then
    warn "未填写解锁 DNS，平台暂未启用。之后使用 smartunlock upstream-add 配置。"
    return
  fi
  printf 'default|primary|%s|%s|\n' "$primary_proto" "$primary" > "$ETC_DIR/upstreams.tsv"
  [[ -n "$backup" ]] && printf 'default|backup|%s|%s|\n' "$backup_proto" "$backup" >> "$ETC_DIR/upstreams.tsv"
  jq -r '.platforms[].id + "|default"' "$APP_DIR/config/platforms.json" > "$ETC_DIR/enabled.tsv"
  chmod 0600 "$ETC_DIR/upstreams.tsv" "$ETC_DIR/enabled.tsv"
}

install_services() {
  local smartdns_bin
  smartdns_bin="$(command -v smartdns)"
  sed "s|@SMARTDNS_BIN@|$smartdns_bin|g" "$WORK_DIR/project/systemd/smartdns-unlock.service" > /etc/systemd/system/smartdns-unlock.service
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-update.service" /etc/systemd/system/smartunlock-update.service
  install -m 0644 "$WORK_DIR/project/systemd/smartunlock-update.timer" /etc/systemd/system/smartunlock-update.timer
  systemctl disable --now smartdns.service 2>/dev/null || true
  systemctl daemon-reload
  systemctl enable smartdns-unlock.service smartunlock-update.timer >/dev/null
  /usr/local/sbin/smartunlock apply
  systemctl start smartunlock-update.timer
}

download_project
install_smartdns
install_project
write_smartdns_config
configure_unlock_dns
install_services

info "安装完成"
printf '\n常用命令：\n'
printf '  smartunlock list\n'
printf '  smartunlock on streaming\n'
printf '  smartunlock on ai\n'
printf '  smartunlock off netflix\n'
printf '  smartunlock status\n'
