# SmartDNS Unlock

面向 Debian 的 SmartDNS 流媒体与 AI 平台分流工具。项目固定使用 SmartDNS Release 48.4，普通域名使用 TCP 443 测速；解锁域名关闭测速，并交给指定的 DoH、DoT、UDP、TCP、DoQ 或 DoH3 解锁上游。

## 功能

- 39 个内置平台，影视与 AI 分类一键开关
- 每个平台独立域名文件，可以绑定不同解锁上游组
- 每组支持主上游和备用上游；备用项带 SmartDNS `-fallback`
- GitHub Actions 每天构建一次规则并自动提交
- Debian 服务器每天自动拉取规则，失败自动保留或恢复上一版
- 自动备份并将 Debian 系统 DNS 指向本机 SmartDNS，安装验证失败自动恢复
- 原 SmartDNS 配置首次安装时备份为 `smartdns.conf.before-smartunlock`
- 私有仓库使用的 GitHub Token 仅保存在服务器 `/etc/smartdns-unlock/github.env`，权限为 `0600`

## 一键安装

公开仓库无需 Token。在 Debian 上执行：

```bash
curl -fsSL https://raw.githubusercontent.com/zhou1050/smartdns-unlock/main/install.sh | sudo -E bash
```

安装器会自动安装 SmartDNS Release 48.4、备份原 DNS 配置、接管系统 DNS，并引导填写主、备用解锁 DNS 以及各自的 DoH/DoT 协议。SmartDNS 默认只监听本机回环地址；命中启用平台列表的域名走解锁 DNS，其他域名走 Cloudflare 和 Google DoH。

## 私有仓库安装

私有仓库无法匿名下载。建议创建一个只允许读取本仓库 `Contents` 的 Fine-grained personal access token。不要使用拥有全部仓库写权限的经典 Token。

在 Debian 服务器上执行：

```bash
export SMARTUNLOCK_REPOSITORY='OWNER/smartdns-unlock'
read -rsp 'GitHub只读Token: ' GITHUB_TOKEN; echo
export GITHUB_TOKEN

SMARTUNLOCK_CURL_CONFIG="$(mktemp)"
printf 'header = "Authorization: Bearer %s"\n' "$GITHUB_TOKEN" > "$SMARTUNLOCK_CURL_CONFIG"
chmod 600 "$SMARTUNLOCK_CURL_CONFIG"
curl -fsSL --config "$SMARTUNLOCK_CURL_CONFIG" \
  -H 'Accept: application/vnd.github.raw+json' \
  "https://api.github.com/repos/$SMARTUNLOCK_REPOSITORY/contents/install.sh" |
  sudo -E bash

rm -f "$SMARTUNLOCK_CURL_CONFIG"
unset GITHUB_TOKEN
```

安装器会询问主、备用解锁 DNS。输入 DoH 时形如：

```text
https://example.com/dns-query
```

输入 DoT 时形如：

```text
tls://dns.example.com:853
```

如果主 DNS 是 DoT，请在安装前指定协议：

```bash
export UNLOCK_PRIMARY_PROTO=dot
```

支持的协议名称：`udp`、`tcp`、`dot`、`doh`、`doq`、`doh3`。

## 平台控制

```bash
# 查看所有平台
smartunlock list

# 一键开启或关闭整个分类
smartunlock on streaming
smartunlock on ai
smartunlock off streaming
smartunlock off ai

# 单独控制平台
smartunlock on netflix
smartunlock off netflix
smartunlock on openai

# 状态和立即更新
smartunlock status
smartunlock update
```

安装时填写了解锁 DNS 后，默认开启所有影视和 AI 平台。

## 多条解锁线路

建立日本线路组，并让 Netflix 使用它：

```bash
smartunlock upstream-add jp primary doh 'https://jp-primary.example/dns-query'
smartunlock upstream-add jp backup dot 'tls://jp-backup.example:853'
smartunlock on netflix jp
```

建立美国 AI 线路组：

```bash
smartunlock upstream-add ai_us primary doh 'https://ai-primary.example/dns-query'
smartunlock upstream-add ai_us backup doh 'https://ai-backup.example/dns-query'
smartunlock on ai ai_us
```

查看上游：

```bash
smartunlock upstream-list
```

## 内置平台

影视：Netflix、Disney+、YouTube、Prime Video、Max/HBO Max、Hulu、Apple TV+、Spotify、TikTok、DAZN、BBC iPlayer、Paramount+、Peacock、Crunchyroll、ABEMA、Bahamut、Bilibili、iQIYI、Viu、TVB。

AI：ChatGPT/OpenAI、Claude、Gemini、GitHub Copilot、Microsoft Copilot、Perplexity、Grok、Poe、Midjourney、Suno、DeepSeek、Cursor、Canva、Notion AI、Character.AI、Runway、Mistral、Hugging Face、OpenRouter。

## 自定义域名

在 `rules/custom/<平台ID>.txt` 中每行写一个域名。GitHub Actions 会将其合并进对应规则。例如：

```text
# rules/custom/netflix.txt
example.netflix-related-domain.com
```

## 定时任务

- GitHub Actions：每天 `19:17 UTC` 构建规则，可在 Actions 页面手动运行。
- Debian：每天本机时间 `05:17` 后随机延迟最多 20 分钟拉取仓库。

检查服务器定时器：

```bash
systemctl list-timers smartunlock-update.timer
journalctl -u smartunlock-update.service -n 100 --no-pager
```

## 说明

DNS 解锁是否成功取决于解锁 DNS 服务商。部分 AI 平台会校验实际出口 IP、账号地区或支付地区；如果服务商不提供对应代理能力，只有 DNS 分流可能仍无法使用。

规则主要从 [v2fly/domain-list-community](https://github.com/v2fly/domain-list-community) 构建，临时拉取失败时会保留上一版生成结果。
