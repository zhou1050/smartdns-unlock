package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zhou1050/smartdns-unlock/internal/app"
)

var version = "dev"
var commit = "unknown"
var buildTime = "unknown"

func fatal(v any) { fmt.Fprintln(os.Stderr, "错误：", v); os.Exit(1) }
func usage() {
	fmt.Print(`SmartDNS Unlock

smartunlock daemon
smartunlock native-scan
smartunlock check
smartunlock health-check
smartunlock update
smartunlock upgrade
smartunlock version
smartunlock status
smartunlock list
smartunlock auto <平台|streaming|ai|all>
smartunlock on <平台|streaming|ai|all> [primary|backup]
smartunlock off <平台|streaming|ai|all>
smartunlock apply
smartunlock uninstall [-y|--yes]
`)
}
func signalDaemon(cfg app.Config) { _ = exec.Command("systemctl", "kill", "--kill-whom=main", "-s", "HUP", cfg.ServiceName).Run() }

func confirmUninstall() bool {
	fmt.Print("将停止 SmartUnlock、恢复安装前 DNS，并清理本项目文件。确认卸载？[y/N]: ")
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() { return false }
	v := strings.ToLower(strings.TrimSpace(s.Text()))
	return v == "y" || v == "yes"
}

func main() {
	cfg, err := app.LoadConfig("")
	if err != nil { fatal(err) }
	cmd := "help"
	if len(os.Args) > 1 { cmd = os.Args[1] }
	if cmd == "uninstall" {
		if os.Geteuid() != 0 { fatal("卸载需要 root 权限") }
		force := false
		for _, a := range os.Args[2:] { if a == "-y" || a == "--yes" { force = true } }
		if !force && !confirmUninstall() { fmt.Println("已取消卸载"); return }
		warnings := app.Uninstall(cfg)
		for _, w := range warnings { fmt.Fprintln(os.Stderr, "警告：", w) }
		fmt.Println("SmartUnlock 已卸载；安装前 DNS/SmartDNS 状态已按可用备份恢复。")
		return
	}
	if cmd == "version" {
		fmt.Printf("smartunlock %s commit=%s built=%s\n", version, commit, buildTime)
		return
	}
	if err := cfg.Validate(); err != nil { fatal(err) }
	ctx := context.Background()
	if cmd == "upgrade" {
		c, cancel := context.WithTimeout(ctx, 3*time.Minute); defer cancel()
		changed, digest, e := app.Upgrade(c, cfg); if e != nil { fatal(e) }
		if changed { fmt.Printf("程序升级完成，SHA256=%s\n", digest) } else { fmt.Printf("已是最新程序，SHA256=%s\n", digest) }
		return
	}
	m, err := app.NewManager(cfg)
	if err != nil { fatal(err) }
	switch cmd {
	case "daemon":
		if err := app.RunDaemon(ctx, cfg); err != nil { fatal(err) }
	case "native-scan":
		c, cancel := context.WithTimeout(ctx, 8*time.Minute); defer cancel()
		if err := m.NativeScan(c); err != nil { fatal(err) }
		fmt.Println("原生解锁检测完成")
	case "check":
		c, cancel := context.WithTimeout(ctx, 12*time.Minute); defer cancel()
		r, e := m.PlatformCheck(c, true); if e != nil { fatal(e) }
		summary := app.Summary(r)
		fmt.Println(summary)
		if e := app.NotifyTelegram(c, cfg, "🔍 SmartDNS 手动解锁检测\n"+summary); e != nil {
			fmt.Fprintln(os.Stderr, "警告：Telegram 发送失败：", e)
		}
	case "health-check":
		c, cancel := context.WithTimeout(ctx, 30*time.Second); defer cancel()
		p, b, e := m.HealthCheck(c); if e != nil { fatal(e) }; fmt.Printf("主=%v 备用=%v\n", p, b)
	case "update":
		c, cancel := context.WithTimeout(ctx, 2*time.Minute); defer cancel()
		if e := m.SyncRules(c); e != nil { fatal(e) }; signalDaemon(cfg); fmt.Println("规则已更新")
	case "apply":
		if e := m.Apply(false); e != nil { fatal(e) }; signalDaemon(cfg); fmt.Println("配置已应用")
	case "status":
		fmt.Println(m.StatusText())
	case "list":
		for _, p := range app.Platforms {
			route := m.State.Routes[p.ID]; if route == "" { route = "primary" }
			fmt.Printf("%-20s %-24s %-10s %-8s %s\n", p.ID, p.Name, p.Category, m.State.RouteMode(p.ID), route)
		}
	case "auto":
		if len(os.Args) < 3 { fatal("用法: smartunlock auto <平台|分类>") }
		if e := m.SetAuto(os.Args[2]); e != nil { fatal(e) }; signalDaemon(cfg)
		fmt.Printf("%s 已设为自动模式\n", os.Args[2])
	case "on":
		if len(os.Args) < 3 { fatal("用法: smartunlock on <平台|分类> [primary|backup]") }
		route := "primary"; if len(os.Args) > 3 { route = strings.ToLower(os.Args[3]) }
		if route != "primary" && route != "backup" { fatal("仅支持 primary 或 backup") }
		if e := m.SetRoute(os.Args[2], route); e != nil { fatal(e) }; signalDaemon(cfg)
		fmt.Printf("%s 已设为手动模式，线路=%s\n", os.Args[2], route)
	case "off":
		if len(os.Args) < 3 { fatal("用法: smartunlock off <平台|分类>") }
		if e := m.SetRoute(os.Args[2], "off"); e != nil { fatal(e) }; signalDaemon(cfg)
		fmt.Printf("%s 已设为手动模式，状态=off\n", os.Args[2])
	default:
		usage()
	}
}
