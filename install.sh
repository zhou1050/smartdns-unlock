#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="${SMARTUNLOCK_REPOSITORY:-zhou1050/smartdns-unlock}"
SMARTDNS_TAG="${SMARTDNS_TAG:-Release48.4}"
CONFIG_DIR=/etc/smartdns-unlock
STATE_DIR=/var/lib/smartdns-unlock
BIN=/usr/local/bin/smartunlock
SERVICE=/etc/systemd/system/smartunlock.service
PRIMARY="${UNLOCK_PRIMARY:-}"
BACKUP="${UNLOCK_BACKUP:-}"
PRIMARY_PROTO="${UNLOCK_PRIMARY_PROTO:-}"
BACKUP_PROTO="${UNLOCK_BACKUP_PROTO:-}"
CHECK_INTERVAL="${CHECK_INTERVAL:-5m}"
RULE_UPDATE_TIME="${RULE_UPDATE_TIME:-05:17}"
PLATFORM_CHECK_TIME="${PLATFORM_CHECK_TIME:-06:17}"
TG_REPORT_TIME="${TG_REPORT_TIME:-09:00}"
AUTO_NATIVE_DETECT="${AUTO_NATIVE_DETECT:-1}"
TG_BOT_TOKEN="${TG_BOT_TOKEN:-}"
TG_CHAT_ID="${TG_CHAT_ID:-}"
TGBOT_CONFIG="${TGBOT:-}"
WORK="$(mktemp -d /tmp/smartunlock-install.XXXXXX)"
DNS_PREPARED=0
DNS_OK=0

info(){ printf '\033[1;32m[SmartUnlock]\033[0m %s\n' "$*"; }
warn(){ printf '\033[1;33m[SmartUnlock]\033[0m %s\n' "$*" >&2; }
die(){ printf '\033[1;31m[SmartUnlock]\033[0m %s\n' "$*" >&2; exit 1; }
[[ ${EUID:-$(id -u)} -eq 0 ]] || die '请使用 sudo -E bash 运行'
[[ -r /etc/os-release ]] || die '无法识别系统'
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == debian ]] || die '当前仅支持 Debian'

cleanup(){
  local rc=$?
  if [[ $DNS_PREPARED == 1 && $DNS_OK != 1 ]]; then
    warn '安装未完成，恢复原系统 DNS'
    systemctl disable --now smartunlock.service >/dev/null 2>&1 || true
    if [[ -f "$WORK/resolv.link" ]]; then
      rm -f /etc/resolv.conf; ln -s "$(cat "$WORK/resolv.link")" /etc/resolv.conf
    elif [[ -f "$WORK/resolv.conf" ]]; then
      rm -f /etc/resolv.conf; cp -a "$WORK/resolv.conf" /etc/resolv.conf
    fi
    [[ -f "$WORK/resolved.enabled" ]] && systemctl enable systemd-resolved.service >/dev/null 2>&1 || true
    [[ -f "$WORK/resolved.active" ]] && systemctl start systemd-resolved.service >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK"
  exit "$rc"
}
trap cleanup EXIT

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl tar gzip build-essential libssl-dev dnsutils >/dev/null

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "暂不支持架构：$(uname -m)" ;;
esac

install_binary(){
  local url="https://github.com/$REPOSITORY/releases/download/edge/smartunlock-linux-$ARCH"
  info "下载 smartunlock 单文件二进制 ($ARCH)"
  if curl -fL --retry 3 --connect-timeout 10 "$url" -o "$WORK/smartunlock"; then
    install -m 0755 "$WORK/smartunlock" "$BIN"
    return
  fi
  warn '预编译二进制暂不可用，使用源码编译兜底'
  if ! command -v go >/dev/null 2>&1; then
    apt-get install -y -qq golang-go >/dev/null || die '无法安装 Go 编译器'
  fi
  curl -fsSL --retry 3 "https://github.com/$REPOSITORY/archive/refs/heads/main.tar.gz" -o "$WORK/src.tar.gz"
  mkdir "$WORK/src"
  tar -xzf "$WORK/src.tar.gz" --strip-components=1 -C "$WORK/src"
  (cd "$WORK/src" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$WORK/smartunlock" ./cmd/smartunlock)
  install -m 0755 "$WORK/smartunlock" "$BIN"
}

install_smartdns(){
  if command -v smartdns >/dev/null 2>&1 && [[ "${SMARTDNS_REINSTALL:-0}" != 1 ]]; then
    info "沿用现有 SmartDNS：$(smartdns -v 2>&1 | head -1)"
    return
  fi
  info "安装 SmartDNS $SMARTDNS_TAG"
  curl -fsSL --retry 3 "https://github.com/pymumu/smartdns/archive/refs/tags/$SMARTDNS_TAG.tar.gz" -o "$WORK/smartdns.tar.gz"
  mkdir "$WORK/smartdns"
  tar -xzf "$WORK/smartdns.tar.gz" --strip-components=1 -C "$WORK/smartdns"
  make -C "$WORK/smartdns" -j"$(nproc)" >/dev/null
  make -C "$WORK/smartdns" install >/dev/null
  command -v smartdns >/dev/null 2>&1 || die 'SmartDNS 安装失败'
}

detect_proto(){
  case "$1" in
    https://*) echo doh;; tls://*|*:853) echo dot;; tcp://*) echo tcp;; quic://*) echo doq;; h3://*) echo doh3;; '') echo '';; *) echo udp;;
  esac
}

collect_config(){
  if [[ -n "$TGBOT_CONFIG" && ( -z "$TG_BOT_TOKEN" || -z "$TG_CHAT_ID" ) ]]; then
    [[ "$TGBOT_CONFIG" == *'|'* ]] || die "TGBOT 应为 BOT_TOKEN|CHAT_ID"
    TG_BOT_TOKEN="${TGBOT_CONFIG%%|*}"; TG_CHAT_ID="${TGBOT_CONFIG#*|}"
  fi
  if [[ -z "$PRIMARY" && -r /dev/tty ]]; then
    printf '\n主解锁 DNS（可留空）：' >/dev/tty; IFS= read -r PRIMARY </dev/tty || true
  fi
  if [[ -n "$PRIMARY" && -z "$BACKUP" && -r /dev/tty ]]; then
    printf '备用解锁 DNS（可留空）：' >/dev/tty; IFS= read -r BACKUP </dev/tty || true
  fi
  PRIMARY_PROTO="${PRIMARY_PROTO:-$(detect_proto "$PRIMARY")}"; BACKUP_PROTO="${BACKUP_PROTO:-$(detect_proto "$BACKUP")}";
  for v in "$PRIMARY" "$BACKUP" "$TG_BOT_TOKEN" "$TG_CHAT_ID"; do [[ "$v" != *$'\n'* && "$v" != *$'\r'* ]] || die '配置值不能包含换行'; done
  install -d -m 0755 "$CONFIG_DIR" "$STATE_DIR"
  cat > "$CONFIG_DIR/config.env" <<CFG
SMARTUNLOCK_REPOSITORY=$REPOSITORY
UNLOCK_PRIMARY_PROTO=$PRIMARY_PROTO
UNLOCK_PRIMARY=$PRIMARY
UNLOCK_BACKUP_PROTO=$BACKUP_PROTO
UNLOCK_BACKUP=$BACKUP
CHECK_INTERVAL=$CHECK_INTERVAL
RULE_UPDATE_TIME=$RULE_UPDATE_TIME
PLATFORM_CHECK_TIME=$PLATFORM_CHECK_TIME
TG_REPORT_TIME=$TG_REPORT_TIME
TG_BOT_TOKEN=$TG_BOT_TOKEN
TG_CHAT_ID=$TG_CHAT_ID
AUTO_NATIVE_DETECT=$([[ "$AUTO_NATIVE_DETECT" == 0 ]] && echo false || echo true)
SMARTDNS_BIN=$(command -v smartdns)
CFG
  chmod 0600 "$CONFIG_DIR/config.env"
}

backup_dns(){
  install -d -m 0700 "$CONFIG_DIR/backups/system-dns-original"
  if [[ -L /etc/resolv.conf ]]; then
    readlink /etc/resolv.conf > "$WORK/resolv.link"
    readlink /etc/resolv.conf > "$CONFIG_DIR/backups/system-dns-original/resolv.link"
    cp -L /etc/resolv.conf "$CONFIG_DIR/backups/system-dns-original/resolv.conf" 2>/dev/null || true
  elif [[ -f /etc/resolv.conf ]]; then
    cp -a /etc/resolv.conf "$WORK/resolv.conf"
    cp -a /etc/resolv.conf "$CONFIG_DIR/backups/system-dns-original/resolv.conf"
  fi
  systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null && { touch "$WORK/resolved.enabled"; touch "$CONFIG_DIR/backups/system-dns-original/resolved.enabled"; }
  systemctl is-active --quiet systemd-resolved.service 2>/dev/null && { touch "$WORK/resolved.active"; touch "$CONFIG_DIR/backups/system-dns-original/resolved.active"; }
}

install_service(){
  cat > "$SERVICE" <<'UNIT'
[Unit]
Description=SmartDNS Unlock daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/smartunlock daemon
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl disable --now smartdns.service smartdns-unlock.service 2>/dev/null || true
  systemctl enable smartunlock.service >/dev/null
}

install_binary
install_smartdns
collect_config

info '安装阶段检测服务器原生解锁能力'
"$BIN" native-scan || warn '原生检测未完整完成；未判定平台会保守使用解锁 DNS'

backup_dns
DNS_PREPARED=1
if systemctl is-active --quiet systemd-resolved.service 2>/dev/null || systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null; then
  systemctl disable --now systemd-resolved.service >/dev/null 2>&1 || die '无法停止 systemd-resolved'
fi
install_service
systemctl restart smartunlock.service

info '等待 SmartDNS 启动并同步规则'
ok=0
for _ in $(seq 1 45); do
  if dig @127.0.0.1 cloudflare.com A +time=2 +tries=1 +short 2>/dev/null | grep -q .; then ok=1; break; fi
  sleep 1
done
[[ $ok == 1 ]] || { journalctl -u smartunlock.service -n 80 --no-pager >&2 || true; die 'SmartDNS 本机查询验证失败'; }

rm -f /etc/resolv.conf
cat > /etc/resolv.conf <<'RESOLV'
# Managed by smartunlock
nameserver 127.0.0.1
nameserver ::1
options timeout:2 attempts:2
RESOLV
chmod 0644 /etc/resolv.conf
getent ahostsv4 github.com >/dev/null 2>&1 || die '系统 DNS 接管验证失败'
DNS_OK=1

info '执行首次综合解锁复检'
"$BIN" check || warn '首次综合复检未完整完成，可稍后手动执行 smartunlock check'

info '安装完成'
printf '\n长期保留的核心文件：\n  /usr/local/bin/smartunlock\n  /etc/smartdns-unlock/config.env\n  /var/lib/smartdns-unlock/state.json\n  /var/lib/smartdns-unlock/rules.json\n  /etc/systemd/system/smartunlock.service\n\n'
printf '常用命令：smartunlock status | list | check | update | health-check\n'
