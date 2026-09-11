# SmartDNS Unlock

Debian 上的 SmartDNS 流媒体 / AI 平台智能分流工具。普通域名走公共 DNS；需要解锁的平台按规则交给指定的解锁 DNS。项目固定使用 SmartDNS Release 48.4。

## 核心功能

- 内置 39 个影视与 AI 平台，可按平台或分类开关。
- 主 / 备用解锁 DNS，支持 UDP、TCP、DoT、DoH、DoQ、DoH3。
- **安装时先检测服务器原生解锁能力**：明确原生可用的平台保持本地直出；失败或无法可靠判定的平台自动走解锁 DNS。
- **每天复检服务器当前最终配置的综合解锁能力**，不会为了复检绕过已经配置的解锁 DNS。
- 如果主解锁 DNS“能解析但平台仍不解锁”，会单独尝试备用 DNS 的实际平台能力；主备都失败则保留失败状态并 Telegram 报警。
- 解锁 DNS 网络健康检查、规则每日同步、失败回滚、Telegram 状态通知。
- 安装前自动备份系统 DNS 和原 SmartDNS 配置；安装验证失败自动恢复系统 DNS。

> 自动原生优先只采用能够明确判断“可用 / 不可用”的平台探针。无法可靠判断的平台默认走解锁 DNS，避免误判导致平台不可用。

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/zhou1050/smartdns-unlock/main/install.sh | sudo -E bash
```

也可以提前传入参数：

```bash
UNLOCK_PRIMARY='https://主解锁DNS/dns-query' \
UNLOCK_BACKUP='tls://备用解锁DNS:853' \
TGBOT='BOT_TOKEN|CHAT_ID' \
TG_REPORT_TIME='09:00' \
bash <(curl -fsSL https://raw.githubusercontent.com/zhou1050/smartdns-unlock/main/install.sh)
```

`AUTO_NATIVE_DETECT=0` 可关闭安装时的原生解锁检测。未设置时默认开启。

## 自动解锁逻辑

安装时：

1. 先用服务器当前原生网络 / DNS 测试真实平台解锁能力。
2. 原生明确通过的平台不写入 SmartDNS 解锁规则，优先本地直出。
3. 原生失败或无法判定的平台自动加入解锁 DNS 分流。
4. SmartDNS 接管后再次按最终配置做综合复检，并通过 Telegram 汇报结果。

每日复检：

1. 直接测试服务器**当前实际配置**的综合解锁能力。
2. 原本本地直出的平台失效时，自动切入默认解锁 DNS 并复测。
3. 已走默认解锁 DNS 但实际平台仍失败时，单独测试备用 DNS；备用能用则该平台自动切到备用线路。
4. 主、备用都不能解锁时不伪装成功，Telegram 会列出仍未解锁的平台。

## 常用命令

```bash
smartunlock list
smartunlock on netflix
smartunlock on streaming
smartunlock on ai
smartunlock off netflix
smartunlock upstream-list
smartunlock status
smartunlock update
smartunlock health-check
smartunlock health-report

# 手动立即执行一次综合平台复检 / 自动修复
/opt/smartdns-unlock/scripts/auto_unlock.sh daily
```

## 安装后的主要文件

| 路径 | 用途 |
|---|---|
| `/usr/local/sbin/smartunlock` | 日常管理命令入口 |
| `/opt/smartdns-unlock/bin/smartunlock` | `smartunlock` 主程序源文件 |
| `/opt/smartdns-unlock/config/platforms.json` | 平台清单、名称、分类和规则来源 |
| `/opt/smartdns-unlock/scripts/build_rules.py` | 构建平台域名规则 |
| `/opt/smartdns-unlock/scripts/check_upstreams.py` | 检测主 / 备用解锁 DNS 网络健康 |
| `/opt/smartdns-unlock/scripts/platform_check.py` | 判断真实平台是否解锁 |
| `/opt/smartdns-unlock/scripts/auto_unlock.sh` | 每日综合复检和自动切换逻辑 |
| `/opt/smartdns-unlock/vendor/region-restriction-check.sh` | 安装时固定版本的平台实际可用性探针 |
| `/etc/smartdns/smartdns.conf` | SmartDNS 主配置，由本项目管理 |
| `/etc/smartdns/smartdns.conf.before-smartunlock` | 安装前原 SmartDNS 配置备份（存在原配置时生成） |
| `/etc/smartdns-unlock/upstreams.tsv` | 解锁 DNS 线路组配置 |
| `/etc/smartdns-unlock/enabled.tsv` | 当前需要 SmartDNS 解锁的平台及所属线路组 |
| `/etc/smartdns-unlock/auto-native.tsv` | 安装检测后仍优先本地直出的平台 |
| `/etc/smartdns-unlock/rules/` | 当前平台域名规则 |
| `/etc/smartdns-unlock/generated/` | 自动生成并加载到 SmartDNS 的配置 |
| `/etc/smartdns-unlock/platform-check.json` | 最近一次综合平台检测结果 |
| `/etc/smartdns-unlock/native-platform-check.json` | 安装时原生解锁检测结果 |
| `/etc/smartdns-unlock/health.env` | 健康检查和 Telegram 配置，权限 `0600` |
| `/etc/smartdns-unlock/github.env` | 仓库 / Token 配置，权限 `0600` |
| `/etc/smartdns-unlock/backups/` | 系统 DNS 和生成配置的备份 / 回滚数据 |
| `/var/log/smartdns/smartdns.log` | SmartDNS 日志 |

## 定时任务

- `smartunlock-health.timer`：按 `CHECK_INTERVAL` 检查解锁 DNS 网络健康。
- `smartunlock-update.timer`：每天本机时间 `05:17` 后随机延迟最多 20 分钟同步规则。
- `smartunlock-platform.timer`：每天本机时间 `06:17` 后随机延迟最多 20 分钟做综合平台解锁复检和自动修复。
- `smartunlock-report.timer`：按 `TG_REPORT_TIME` 发送 Telegram 日报。

查看状态：

```bash
systemctl list-timers 'smartunlock-*'
journalctl -u smartunlock-platform.service -n 100 --no-pager
journalctl -u smartunlock-update.service -n 100 --no-pager
```

## 说明

DNS 解锁能力最终取决于解锁 DNS 服务商。部分 AI 平台还会校验实际出口 IP、账号地区或支付地区，因此 DNS 分流并不能保证所有账号场景都可用。

规则主要从 `v2fly/domain-list-community` 构建；平台实际可用性检测使用固定版本的 `1-stream/RegionRestrictionCheck` 探针，避免上游脚本变化直接影响已安装服务器。
