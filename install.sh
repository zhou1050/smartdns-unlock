#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="${SMARTUNLOCK_REPOSITORY:-zhou1050/smartdns-unlock}"
SMARTDNS_TAG="${SMARTDNS_TAG:-Release48.4}"
SMARTUNLOCK_BINARY="${SMARTUNLOCK_BINARY:-}"
CONFIG_DIR=/etc/smartdns-unlock
STATE_DIR=/var/lib/smartdns-unlock
BIN=/usr/local/bin/smartunlock
SERVICE=/etc/systemd/system/smartunlock.service
SMARTDNS_MANAGED_MARKER="$CONFIG_DIR/smartdns-managed.version"
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
BINARY_UPGRADE_PENDING=0

info(){ printf '\033[1;32m[SmartUnlock]\033[0m %s\n' "$*"; }
warn(){ printf '\033[1;33m[SmartUnlock]\033[0m %s\n' "$*" >&2; }
die(){ printf '\033[1;31m[SmartUnlock]\033[0m %s\n' "$*" >&2; exit 1; }
unlock_resolv(){ chattr -i /etc/resolv.conf >/dev/null 2>&1 || true; }
[[ ${EUID:-$(id -u)} -eq 0 ]] || die '请使用 sudo -E bash 运行'
[[ -r /etc/os-release ]] || die '无法识别系统'
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == debian ]] || die '当前仅支持 Debian'

cleanup(){
  local rc=$?
  if [[ $rc != 0 && $BINARY_UPGRADE_PENDING == 1 && -f "$WORK/smartunlock.old" ]]; then
    warn '程序升级未完成，恢复旧版 smartunlock'
    rm -f "$BIN"
    cp -a "$WORK/smartunlock.old" "$BIN"
    systemctl restart smartunlock.service >/dev/null 2>&1 || true
  fi
  if [[ $DNS_PREPARED == 1 && $DNS_OK != 1 ]]; then
    warn '安装未完成，恢复本次安装前 DNS'
    systemctl disable --now smartunlock.service >/dev/null 2>&1 || true
    unlock_resolv
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
apt-get install -y -qq ca-certificates curl tar gzip build-essential libssl-dev pkg-config dnsutils e2fsprogs >/dev/null
install -d -m 0755 "$CONFIG_DIR" "$STATE_DIR"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "暂不支持架构：$(uname -m)" ;;
esac

backup_smartdns(){
  local d="$CONFIG_DIR/backups/smartdns-original" p fragment
  install -d -m 0700 "$d"
  [[ -f "$d/preexisting" || -f "$d/installed-by-smartunlock" ]] && return 0
  if command -v smartdns >/dev/null 2>&1; then
    touch "$d/preexisting"
    p="$(command -v smartdns)"
    printf '%s\n' "$p" > "$d/binary.path"
    cp -a "$p" "$d/smartdns.bin" || true
    if [[ -f /etc/smartdns/smartdns.conf ]]; then touch "$d/config.existed"; cp -a /etc/smartdns/smartdns.conf "$d/smartdns.conf"; fi
    [[ -f /etc/default/smartdns ]] && cp -a /etc/default/smartdns "$d/default.smartdns" || true
    [[ -f /etc/init.d/smartdns ]] && cp -a /etc/init.d/smartdns "$d/init.smartdns" || true
    fragment="$(systemctl show -p FragmentPath --value smartdns.service 2>/dev/null || true)"
    if [[ -n "$fragment" && -f "$fragment" ]]; then printf '%s\n' "$fragment" > "$d/service.path"; cp -a "$fragment" "$d/smartdns.service" || true; fi
    systemctl is-enabled --quiet smartdns.service 2>/dev/null && touch "$d/service.enabled" || true
    systemctl is-active --quiet smartdns.service 2>/dev/null && touch "$d/service.active" || true
  else
    touch "$d/installed-by-smartunlock"
  fi
  return 0
}

record_smartdns_install(){
  local d="$CONFIG_DIR/backups/smartdns-original" fragment
  [[ -f "$d/installed-by-smartunlock" ]] || return 0
  command -v smartdns > "$d/binary.path"
  systemctl daemon-reload >/dev/null 2>&1 || true
  fragment="$(systemctl show -p FragmentPath --value smartdns.service 2>/dev/null || true)"
  [[ -n "$fragment" ]] && printf '%s\n' "$fragment" > "$d/service.path" || true
  return 0
}

install_binary(){
  local url="https://github.com/$REPOSITORY/releases/download/edge/smartunlock-linux-$ARCH"
  if [[ -n "$SMARTUNLOCK_BINARY" ]]; then
    [[ -f "$SMARTUNLOCK_BINARY" ]] || die "指定的 SMARTUNLOCK_BINARY 不存在：$SMARTUNLOCK_BINARY"
    info "安装指定的 smartunlock 二进制：$SMARTUNLOCK_BINARY"
    install -m 0755 "$SMARTUNLOCK_BINARY" "$BIN"
    return
  fi
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

upgrade_existing(){
  info '检测到现有 SmartUnlock，仅升级程序并保留配置、状态和 DNS 环境'
  cp -a "$BIN" "$WORK/smartunlock.old"
  BINARY_UPGRADE_PENDING=1
  rm -f "$BIN"
  install_binary
  "$BIN" version >/dev/null || die '新 smartunlock 二进制验证失败'
  systemctl restart smartunlock.service || die '新版本服务启动失败'
  for _ in $(seq 1 30); do
    if dig @127.0.0.1 cloudflare.com A +time=2 +tries=1 +short 2>/dev/null | grep -q .; then
      BINARY_UPGRADE_PENDING=0
      info "程序升级完成：$($BIN version)"
      return 0
    fi
    sleep 1
  done
  die '升级后本机 DNS 验证失败'
}

smartdns_compatible(){
  local v major
  [[ -x "$(command -v smartdns 2>/dev/null || true)" ]] || return 1

  # A SmartDNS binary installed by this installer has already been validated
  # against the generated config syntax. The marker also avoids rebuilding it
  # on every SmartUnlock upgrade.
  if [[ -r "$SMARTDNS_MANAGED_MARKER" ]] && [[ "$(cat "$SMARTDNS_MANAGED_MARKER" 2>/dev/null || true)" == "$SMARTDNS_TAG" ]]; then
    return 0
  fi

  v="$(smartdns -v 2>&1 | head -1 || true)"
  # Debian 12 ships smartdns 40+dfsg-1. It starts with our generated config but
  # cannot reliably register the modern grouped upstream/domain-rule options,
  # which results in 'total server number 0'. Refuse known old distro releases
  # rather than letting installation appear successful and then lose DNS.
  if [[ "$v" =~ ^smartdns[[:space:]]+([0-9]+)\+dfsg ]]; then
    major="${BASH_REMATCH[1]}"
    if [[ "$major" =~ ^[0-9]+$ ]] && (( major < 48 )); then
      return 1
    fi
  fi
  return 0
}

install_smartdns(){
  local current=''
  if command -v smartdns >/dev/null 2>&1; then
    current="$(smartdns -v 2>&1 | head -1 || true)"
  fi
  if [[ -n "$current" && "${SMARTDNS_REINSTALL:-0}" != 1 ]] && smartdns_compatible; then
    info "沿用现有 SmartDNS：$current"
    return
  fi
  if [[ -n "$current" ]]; then
    warn "现有 SmartDNS 不满足稳定运行要求：$current；自动升级到 $SMARTDNS_TAG（原版本已备份，卸载可恢复）"
  else
    info "安装 SmartDNS $SMARTDNS_TAG"
  fi
  curl -fsSL --retry 3 "https://github.com/pymumu/smartdns/archive/refs/tags/$SMARTDNS_TAG.tar.gz" -o "$WORK/smartdns.tar.gz"
  mkdir "$WORK/smartdns"
  tar -xzf "$WORK/smartdns.tar.gz" --strip-components=1 -C "$WORK/smartdns"
  make -C "$WORK/smartdns" -j"$(nproc)" >/dev/null
  make -C "$WORK/smartdns" install >/dev/null
  command -v smartdns >/dev/null 2>&1 || die 'SmartDNS 安装失败'
  printf '%s\n' "$SMARTDNS_TAG" > "$SMARTDNS_MANAGED_MARKER"
  info "SmartDNS 已准备：$(smartdns -v 2>&1 | head -1)"
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
  if [[ -z "$PRIMARY" && -r /dev/tty ]]; then printf '\n主解锁 DNS（可留空）：' >/dev/tty; IFS= read -r PRIMARY </dev/tty || true; fi
  if [[ -n "$PRIMARY" && -z "$BACKUP" && -r /dev/tty ]]; then printf '备用解锁 DNS（可留空）：' >/dev/tty; IFS= read -r BACKUP </dev/tty || true; fi
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
  local d="$CONFIG_DIR/backups/system-dns-original"
  install -d -m 0700 "$d"

  # Always keep a temporary copy so a failed reinstall can roll back to the
  # state that existed immediately before this run.
  if [[ -L /etc/resolv.conf ]]; then
    readlink /etc/resolv.conf > "$WORK/resolv.link"
  elif [[ -f /etc/resolv.conf ]]; then
    cp -a /etc/resolv.conf "$WORK/resolv.conf"
  fi
  systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null && touch "$WORK/resolved.enabled" || true
  systemctl is-active --quiet systemd-resolved.service 2>/dev/null && touch "$WORK/resolved.active" || true

  # Persistent restore data is first-install-only. Never overwrite the real
  # pre-SmartUnlock DNS backup during an upgrade/reinstall.
  if [[ ! -e "$d/resolv.link" && ! -e "$d/resolv.conf" ]]; then
    if [[ -L /etc/resolv.conf ]]; then
      readlink /etc/resolv.conf > "$d/resolv.link"
      cp -L /etc/resolv.conf "$d/resolv.conf" 2>/dev/null || true
    elif [[ -f /etc/resolv.conf ]]; then
      cp -a /etc/resolv.conf "$d/resolv.conf"
    fi
    systemctl is-enabled --quiet systemd-resolved.service 2>/dev/null && touch "$d/resolved.enabled" || true
    systemctl is-active --quiet systemd-resolved.service 2>/dev/null && touch "$d/resolved.active" || true
  fi
  return 0
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

if [[ -x "$BIN" && -f "$CONFIG_DIR/config.env" && -f "$SERVICE" && "${SMARTUNLOCK_FULL_REINSTALL:-0}" != 1 ]]; then
  upgrade_existing
  exit 0
fi
install_binary
backup_smartdns
install_smartdns
record_smartdns_install
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

unlock_resolv
rm -f /etc/resolv.conf
{
  echo '# Managed by smartunlock'
  echo 'nameserver 127.0.0.1'
  if [[ -r /proc/net/if_inet6 ]] && grep -q '^00000000000000000000000000000001 ' /proc/net/if_inet6; then
    echo 'nameserver ::1'
  fi
  echo 'options timeout:2 attempts:2'
} > /etc/resolv.conf
chmod 0644 /etc/resolv.conf
if chattr +i /etc/resolv.conf 2>/dev/null; then
  info '已锁定 /etc/resolv.conf，防止 DHCP 覆盖 SmartDNS'
else
  warn '当前文件系统不支持锁定 /etc/resolv.conf；DNS 仍已接管，但 DHCP 可能再次覆盖'
fi
getent ahostsv4 github.com >/dev/null 2>&1 || die '系统 DNS 接管验证失败'
DNS_OK=1

info '执行首次综合解锁复检'
"$BIN" check || warn '首次综合复检未完整完成，可稍后手动执行 smartunlock check'

info '安装完成'
printf '\n长期保留的核心文件：\n  /usr/local/bin/smartunlock\n  /etc/smartdns-unlock/config.env\n  /var/lib/smartdns-unlock/state.json\n  /var/lib/smartdns-unlock/rules.json\n  /etc/systemd/system/smartunlock.service\n\n'
printf '常用命令：smartunlock status | list | check | update | upgrade | version | health-check\n'
printf '卸载命令：sudo smartunlock uninstall\n'
