package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zhou1050/smartdns-unlock/internal/app"
)

func fatal(v any) { fmt.Fprintln(os.Stderr, "错误：", v); os.Exit(1) }
func usage() {
	fmt.Print(`SmartDNS Unlock

smartunlock daemon
smartunlock native-scan
smartunlock check
smartunlock health-check
smartunlock update
smartunlock status
smartunlock list
smartunlock on <平台|streaming|ai|all> [primary|backup]
smartunlock off <平台|streaming|ai|all>
smartunlock apply
`)
}
func signalDaemon(cfg app.Config) { _ = exec.Command("systemctl", "kill", "-s", "HUP", cfg.ServiceName).Run() }

func main() {
	cfg, err := app.LoadConfig("")
	if err != nil { fatal(err) }
	if err := cfg.Validate(); err != nil { fatal(err) }
	cmd := "help"
	if len(os.Args) > 1 { cmd = os.Args[1] }
	ctx := context.Background()
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
		r, e := m.PlatformCheck(c, true); if e != nil { fatal(e) }; fmt.Println(app.Summary(r))
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
			fmt.Printf("%-20s %-24s %-10s %s\n", p.ID, p.Name, p.Category, route)
		}
	case "on":
		if len(os.Args) < 3 { fatal("用法: smartunlock on <平台|分类> [primary|backup]") }
		route := "primary"; if len(os.Args) > 3 { route = strings.ToLower(os.Args[3]) }
		if route != "primary" && route != "backup" { fatal("仅支持 primary 或 backup") }
		if e := m.SetRoute(os.Args[2], route); e != nil { fatal(e) }; signalDaemon(cfg)
	case "off":
		if len(os.Args) < 3 { fatal("用法: smartunlock off <平台|分类>") }
		if e := m.SetRoute(os.Args[2], "off"); e != nil { fatal(e) }; signalDaemon(cfg)
	default:
		usage()
	}
}
