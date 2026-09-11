# SmartDNS Unlock

Debian 上的 SmartDNS 流媒体 / AI 平台智能分流工具。服务器端改为 **单 Go 二进制 + 单 systemd 服务**：规则同步、DNS 健康检查、平台复检、主备切换和 Telegram 通知都由 `smartunlock` 自己完成。

## 功能

- 39 个影视 / AI 平台域名规则。
- 主、备用解锁 DNS，支持 UDP、TCP、DoT、DoH、DoQ、DoH3。
- 安装时先检测服务器原生解锁能力，**明确原生可用才保持本地直出**；失败或无法可靠判断的平台默认走解锁 DNS。
- 每天按服务器当前最终配置做综合解锁复检，不刻意绕过已经配置的解锁 DNS。
- 主 DNS 网络故障时自动使用备用；主 DNS 虽然能解析但平台实际仍失败时，会实测备用线路，备用可用则该平台切备用。
- 主备都不能实际解锁时保留失败状态并 Telegram 报警，不伪装成功。
- 每天自动同步 GitHub 规则；新规则应用失败不会影响已有规则缓存。

> 平台“原生优先”只使用程序内能够可靠判断可用性的探针。没有可靠探针的平台不会因为网页能打开就判定为原生解锁，而是保守使用解锁 DNS。

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/zhou1050/smartdns-unlock/main/install.sh | sudo -E bash
```

也可以直接传参数：

```bash
UNLOCK_PRIMARY='https://主解锁DNS/dns-query' \
UNLOCK_BACKUP='tls://备用解锁DNS:853' \
TGBOT='BOT_TOKEN|CHAT_ID' \
TG_REPORT_TIME='09:00' \
bash <(curl -fsSL https://raw.githubusercontent.com/zhou1050/smartdns-unlock/main/install.sh)
```

安装器优先下载 GitHub `edge` Release 中的静态二进制；如果预编译文件暂时不可用，才使用源码编译兜底。`amd64` / `arm64` 均支持。

## 自动逻辑

安装时先在服务器原来的 DNS / 网络环境执行一次 `native-scan`，再启动 SmartDNS 并接管系统 DNS。因此原生检测不会被新配置的解锁 DNS 干扰。

安装完成后由一个 `smartunlock daemon` 统一负责：

- `CHECK_INTERVAL`：检查主 / 备用解锁 DNS 网络健康，默认 `5m`。
- `RULE_UPDATE_TIME`：每天同步规则，默认 `05:17`。
- `PLATFORM_CHECK_TIME`：每天综合平台复检和自动修复，默认 `06:17`。
- `TG_REPORT_TIME`：Telegram 日报，默认 `09:00`。

这些任务**不再创建 4 套 systemd timer/service**。

## 常用命令

```bash
smartunlock status
smartunlock list
smartunlock check
smartunlock health-check
smartunlock update

smartunlock on netflix
smartunlock on netflix backup
smartunlock on streaming
smartunlock on ai
smartunlock off netflix
```

查看服务日志：

```bash
systemctl status smartunlock --no-pager
journalctl -u smartunlock -n 100 --no-pager
```

## 服务器安装后有哪些文件

长期保留的核心文件很少：

| 路径 | 用途 |
|---|---|
| `/usr/local/bin/smartunlock` | 单个静态 Go 主程序，约 6 MB；包含调度、检测、切换、TG 和 SmartDNS 配置生成逻辑 |
| `/etc/smartdns-unlock/config.env` | 主 / 备用 DNS、TG、检测周期和每日时间，权限 `0600` |
| `/var/lib/smartdns-unlock/state.json` | 当前各平台走原生 / 主 DNS / 备用 DNS的状态及最近检测结果 |
| `/var/lib/smartdns-unlock/rules.json` | 所有平台域名规则的单文件本地缓存 |
| `/etc/systemd/system/smartunlock.service` | 唯一的 SmartUnlock systemd 服务 |
| `/etc/smartdns/smartdns.conf` | 由 `smartunlock` 自动生成的 SmartDNS 主配置 |
| `/etc/smartdns-unlock/backups/system-dns-original/` | 安装前系统 DNS 备份，仅用于故障恢复 |
| `/var/log/smartdns/smartdns.log` | SmartDNS 自身日志 |

运行时程序会把 `rules.json` 临时展开到：

```text
/run/smartdns-unlock/
├── upstreams.conf
├── platforms.conf
└── rules/
```

`/run` 是临时目录，重启后会重新生成，**不会在服务器长期堆几十个规则文件**。

## 配置文件

`/etc/smartdns-unlock/config.env` 示例：

```bash
UNLOCK_PRIMARY_PROTO=doh
UNLOCK_PRIMARY=https://dns.example.com/dns-query
UNLOCK_BACKUP_PROTO=dot
UNLOCK_BACKUP=tls://backup.example.com:853
CHECK_INTERVAL=5m
RULE_UPDATE_TIME=05:17
PLATFORM_CHECK_TIME=06:17
TG_REPORT_TIME=09:00
TG_BOT_TOKEN=
TG_CHAT_ID=
AUTO_NATIVE_DETECT=true
```

改完配置后执行：

```bash
systemctl kill -s HUP smartunlock
```

程序会重新读取配置并重建 SmartDNS 运行配置。

## 说明

DNS 解锁最终仍取决于解锁 DNS 服务商。部分 AI 平台还会检查实际出口 IP、账号地区或支付地区，因此仅修改 DNS 不一定能解决所有账号场景。

GitHub 仓库中仍会保留 Go 源码、规则构建脚本和测试文件，方便维护；**这些源码不会安装到服务器**。服务器运行端只需要上面列出的二进制、配置、状态和规则缓存。
