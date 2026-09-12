# SmartDNS Unlock

Debian 上的 SmartDNS 流媒体 / AI 平台智能分流工具。服务器端采用 **单 Go 二进制 + 单 systemd 服务**：规则同步、DNS 健康检查、平台复检、主备切换和 Telegram 通知都由 `smartunlock` 自己完成。

## 功能

- 39 个影视 / AI 平台域名规则。
- 内置 22 个平台能力探针；只有拿到明确可用结果才允许原生直出，其余结果保守走解锁 DNS。
- 主、备用解锁 DNS，支持 UDP、TCP、DoT、DoH、DoQ、DoH3。
- 安装时先检测服务器原生解锁能力，**明确原生可用才保持本地直出**；失败或无法可靠判断的平台默认走解锁 DNS。
- 每天按服务器当前最终配置做综合解锁复检，不刻意绕过已经配置的解锁 DNS。
- 主 DNS 网络故障时自动使用备用；主 DNS 虽然能解析但平台实际仍失败时，会实测备用线路，备用可用则该平台切备用。
- 主备都不能实际解锁时保留失败状态并 Telegram 报警，不伪装成功。
- 每个平台支持自动 / 手动模式：自动模式允许每日检测及健康检查调整线路；手动模式固定指定线路，不被后台任务改动。
- 每天自动同步 GitHub 规则；新规则应用失败不会影响已有规则缓存。
- 多个 CLI 配置变更在短时间连续发生时自动合并重载，避免 SmartDNS 重启风暴和 DNS 短暂断流。
- 内置安全卸载：恢复安装前系统 DNS；服务器原本已有 SmartDNS 时恢复原 SmartDNS，而不是删除它。

> 当前 22 个探针对应 Netflix、Disney+、YouTube、Prime Video、Max、Hulu、Spotify、TikTok、DAZN、BBC iPlayer、Paramount+、Peacock、Crunchyroll、ABEMA、Bahamut、Bilibili、iQIYI、Viu、TVB、ChatGPT/OpenAI、Claude 和 Microsoft Copilot。部分站点没有稳定公开的无认证检测接口，因此程序采用“宁可 unknown、不误判 pass”的策略；`unknown` 不会自动切成原生直出。

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

`SMARTUNLOCK_BINARY=/path/to/smartunlock` 可用于测试、离线部署或固定版本部署，让安装器直接安装指定的本地二进制；普通用户无需设置。

## 自动逻辑

安装时先在服务器原来的 DNS / 网络环境执行一次 `native-scan`，再启动 SmartDNS 并接管系统 DNS。因此原生检测不会被新配置的解锁 DNS 干扰。

安装完成后由一个 `smartunlock daemon` 统一负责：

- `CHECK_INTERVAL`：检查主 / 备用解锁 DNS 网络健康，默认 `5m`。
- `RULE_UPDATE_TIME`：每天同步规则，默认 `05:17`。
- `PLATFORM_CHECK_TIME`：每天综合平台复检和自动修复，默认 `06:17`。
- `TG_REPORT_TIME`：Telegram 日报，默认 `09:00`。

平台检测最多 4 路并发，每个平台有独立超时，不会因为单个平台接口长时间无响应拖死整个 daemon。

这些任务**不再创建 4 套 systemd timer/service**。

## 常用命令

```bash
smartunlock status
smartunlock list
smartunlock check
smartunlock health-check
smartunlock update
smartunlock upgrade
smartunlock version

smartunlock auto netflix
smartunlock auto streaming
smartunlock on netflix
smartunlock on netflix backup
smartunlock on streaming
smartunlock on ai
smartunlock off netflix
```

`auto` 将平台交给每日检测自动维护；`on` 和 `off` 都会进入手动模式。升级前已经存在但没有模式字段的状态默认按自动模式兼容；旧状态中的 `off`，以及无自动探针平台的 `backup`，会识别为原有手动选择。当前没有可靠探针的平台会保留现有线路，不会凭 `unknown` 结果切换。

`update` 只更新平台域名规则；`upgrade` 从 GitHub `edge` Release 下载当前架构的最新程序，校验 `SHA256SUMS` 后原子替换并重启服务。新程序启动或本机 DNS 验证失败时会自动恢复旧二进制。`version` 可查看当前构建版本、commit 和构建时间。

首次从不带 `upgrade` 命令的旧版本升级时，可重新执行一键安装命令；安装器检测到现有安装后只替换程序，保留 DNS 配置、平台路由状态和系统 DNS 环境。之后直接使用 `smartunlock upgrade`。如确需完整重装，可设置 `SMARTUNLOCK_FULL_REINSTALL=1`。

查看服务日志：

```bash
systemctl status smartunlock --no-pager
journalctl -u smartunlock -n 100 --no-pager
```

## 卸载

交互确认后卸载：

```bash
sudo smartunlock uninstall
```

无人值守 / 直接确认：

```bash
sudo smartunlock uninstall -y
```

卸载不是简单删除文件，会尽量恢复安装前状态：

1. 停止并禁用 `smartunlock.service`。
2. 恢复安装前的 `/etc/resolv.conf`；如果安装前使用 `systemd-resolved`，恢复它原来的启用 / 运行状态。
3. 如果安装前服务器**已经有 SmartDNS**，恢复安装前备份的 SmartDNS 二进制、配置和服务状态，不删除原 SmartDNS。
4. 如果 SmartDNS 是本项目安装的，则停止并清理该 SmartDNS 及本项目产生的缓存 / 日志。
5. 删除 `smartunlock` 二进制、状态、规则缓存、临时运行目录和 systemd 服务文件。

安装时的恢复信息保存在 `/etc/smartdns-unlock/backups/`。如果关键备份缺失，卸载程序采用保守策略：**宁可提示警告并保留未知来源的 SmartDNS，也不会直接误删。**

## 服务器安装后有哪些文件

长期保留的核心文件很少：

| 路径 | 用途 |
|---|---|
| `/usr/local/bin/smartunlock` | 单个静态 Go 主程序，约 6 MB；包含调度、检测、切换、TG 和 SmartDNS 配置生成逻辑 |
| `/etc/smartdns-unlock/config.env` | 主 / 备用 DNS、TG、检测周期和每日时间，权限 `0600` |
| `/var/lib/smartdns-unlock/state.json` | 当前各平台走原生 / 主 DNS / 备用 DNS 的状态及最近检测结果 |
| `/var/lib/smartdns-unlock/rules.json` | 所有平台域名规则的单文件本地缓存 |
| `/etc/systemd/system/smartunlock.service` | 唯一的 SmartUnlock systemd 服务 |
| `/etc/smartdns/smartdns.conf` | 由 `smartunlock` 自动生成的 SmartDNS 主配置 |
| `/etc/smartdns-unlock/backups/system-dns-original/` | 安装前系统 DNS 备份，用于卸载 / 故障恢复 |
| `/etc/smartdns-unlock/backups/smartdns-original/` | 安装前 SmartDNS 来源、配置、二进制和服务状态备份；仅用于安全恢复 |
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
systemctl kill --kill-whom=main -s HUP smartunlock.service
```

必须只把 HUP 发给 Go daemon 主进程；不要对整个 `smartunlock.service` cgroup 发送 HUP，否则 SmartDNS 子进程也会收到信号。程序会对短时间连续 HUP 做合并，然后重新读取配置、重排定时任务并有序重建 SmartDNS 运行配置。

## 测试

仓库持续执行三类验证：

- Go 单元测试及静态 `amd64` / `arm64` 编译。
- `go test -race` + 3 分钟 daemon 故障注入 soak test，观察主备切换、goroutine、RSS 和子进程退出。
- 干净 Debian 12 + systemd 端到端测试：安装真实 SmartDNS、原生扫描、系统 DNS 接管、CLI 连续变更、完整健康周期、主/备故障注入、恢复和卸载回滚。

Debian E2E 会先从当前 commit 编译二进制，再通过 `SMARTUNLOCK_BINARY` 安装该精确版本，不依赖 rolling `edge` 的发布时间顺序。

## 说明

DNS 解锁最终仍取决于解锁 DNS 服务商。部分 AI 平台还会检查实际出口 IP、账号地区或支付地区，因此仅修改 DNS 不一定能解决所有账号场景。

GitHub 仓库中仍会保留 Go 源码、规则构建脚本和测试文件，方便维护；**这些源码不会安装到服务器**。服务器运行端只需要上面列出的二进制、配置、状态和规则缓存。
