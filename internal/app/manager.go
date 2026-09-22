package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	Cfg    Config
	Mu     sync.Mutex
	State  State
	Rules  RulesFile
	DNS    *SmartDNSProcess
	OwnDNS bool
}

func NewManager(cfg Config) (*Manager, error) {
	s, e := LoadState(cfg.StatePath)
	if e != nil { return nil, e }
	r, e := LoadRules(cfg.RulesPath)
	if e != nil { r = RulesFile{Version: 1, Rules: map[string][]string{}} }
	return &Manager{Cfg: cfg, State: s, Rules: r, DNS: NewSmartDNSProcess(cfg)}, nil
}

func (m *Manager) SyncRules(ctx context.Context) error {
	m.Mu.Lock(); defer m.Mu.Unlock()
	r, e := SyncRules(ctx, m.Cfg); if e != nil { return e }
	m.Rules = r; m.State.RulesUpdatedAt = r.UpdatedAt
	if e = SaveState(m.Cfg.StatePath, m.State); e != nil { return e }
	return Render(m.Cfg, m.State, m.Rules)
}

func reloadAckPath(cfg Config) string { return filepath.Join(cfg.RuntimeDir, "reload.ack") }

func readReloadAck(path string) string {
	b, err := os.ReadFile(path)
	if err != nil { return "" }
	return strings.TrimSpace(string(b))
}

func writeReloadAck(cfg Config) error {
	return os.WriteFile(reloadAckPath(cfg), []byte(fmt.Sprintf("%d\n", time.Now().UnixNano())), 0644)
}

func waitReloadAck(path, old string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cur := readReloadAck(path); cur != "" && cur != old { return true }
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func waitMainDNSReady(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil { return false }
		id := uint16(time.Now().UnixNano())
		if checkDNSListenerOnce(ctx, "127.0.0.1:53", 1200*time.Millisecond, id) { return true }
		t := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done(): t.Stop(); return false
		case <-t.C:
		}
	}
	return false
}

func (m *Manager) restartDNS() error {
	if m.OwnDNS {
		err := m.DNS.Restart()
		// Platform probes use the process-wide HTTP transport. Once DNS routing
		// changes, discard idle keep-alive connections so the next verification
		// must resolve and connect through the newly selected unlock DNS.
		closeProbeIdleConnections()
		return err
	}
	ackPath := reloadAckPath(m.Cfg)
	oldAck := readReloadAck(ackPath)
	if err := exec.Command("systemctl", "kill", "--kill-whom=main", "-s", "HUP", m.Cfg.ServiceName).Run(); err != nil { return err }
	// External CLI commands must not return merely because SIGHUP was queued.
	// Wait until the daemon has consumed the debounced reload and restarted its
	// SmartDNS child, then verify the main listener can actually resolve through
	// the newly rendered configuration.
	if !waitReloadAck(ackPath, oldAck, 10*time.Second) { return fmt.Errorf("daemon reload acknowledgement timed out") }
	if !waitMainDNSReady(8*time.Second) { return fmt.Errorf("smartdns main listener not ready after daemon reload") }
	closeProbeIdleConnections()
	return nil
}

func (m *Manager) Apply(restart bool) error {
	m.Mu.Lock()
	if len(m.Rules.Rules) == 0 {
		r, e := LoadRules(m.Cfg.RulesPath)
		if e != nil { m.Mu.Unlock(); return e }
		m.Rules = r
	}
	if e := Render(m.Cfg, m.State, m.Rules); e != nil { m.Mu.Unlock(); return e }
	m.Mu.Unlock()
	if restart { return m.restartDNS() }
	return nil
}

// ApplyRouteChange renders the current state and restarts SmartDNS only when
// the effective platform routing file changed. Health state can change without
// changing any active route (for example, an unused backup going down). In
// that case restarting the local DNS listener only creates avoidable lookup
// failures and provides no failover benefit.
//
// The boolean result reports whether the effective routing changed.
func (m *Manager) ApplyRouteChange(restart bool) (bool, error) {
	path := filepath.Join(m.Cfg.RuntimeDir, "platforms.conf")
	before, beforeErr := os.ReadFile(path)
	if err := m.Apply(false); err != nil { return false, err }
	after, err := os.ReadFile(path)
	if err != nil { return false, err }
	changed := beforeErr != nil || !bytes.Equal(before, after)
	if changed && restart {
		if err := m.restartDNS(); err != nil { return true, err }
	}
	return changed, nil
}

func (m *Manager) NativeScan(ctx context.Context) error {
	m.Mu.Lock(); defer m.Mu.Unlock()
	checks := ProbeAll(ctx)
	if m.State.Routes == nil { m.State.Routes = map[string]string{} }
	if m.State.RouteModes == nil { m.State.RouteModes = map[string]string{} }
	for _, p := range Platforms {
		m.State.RouteModes[p.ID] = "auto"
		r := checks[p.ID]
		if !m.Cfg.NativeAutoDetect { m.State.Routes[p.ID] = "primary"; continue }
		if nativeProbeCanSelect(p, r) { m.State.Routes[p.ID] = "native" } else { m.State.Routes[p.ID] = "primary" }
	}
	m.State.LastChecks = checks; m.State.Initialized = true; m.State.LastPlatformScan = time.Now()
	return SaveState(m.Cfg.StatePath, m.State)
}

func (m *Manager) HealthCheck(ctx context.Context) (bool, bool, error) {
	p := false; if m.Cfg.Primary != "" { p = CheckDNSListener(ctx, m.Cfg.HealthPrimaryAddr) }
	b := false; if m.Cfg.Backup != "" { b = CheckDNSListener(ctx, m.Cfg.HealthBackupAddr) }
	m.Mu.Lock()
	oldp, oldb := m.State.PrimaryHealthy, m.State.BackupHealthy
	m.State.PrimaryHealthy = p; m.State.BackupHealthy = b
	e := SaveState(m.Cfg.StatePath, m.State); changed := oldp != p || oldb != b
	m.Mu.Unlock()
	if e != nil { return p, b, e }
	if changed {
		routeChanged, e := m.ApplyRouteChange(true)
		if e != nil { return p, b, e }
		msg := fmt.Sprintf("SmartDNS 上游健康变化：主=%v 备用=%v", p, b)
		if !routeChanged { msg += "\n有效分流未变化，SmartDNS 未重启" }
		_ = NotifyTelegram(ctx, m.Cfg, msg)
	}
	return p, b, nil
}

func probeSelected(ctx context.Context, ids []string) map[string]ProbeResult {
	out := make(map[string]ProbeResult, len(ids)); var mu sync.Mutex; var wg sync.WaitGroup; sem := make(chan struct{}, 4)
	for _, id := range ids {
		p, ok := PlatformByID(id); if !ok || p.Probe == "" { continue }
		wg.Add(1)
		go func(p Platform) {
			defer wg.Done()
			select { case sem <- struct{}{}: case <-ctx.Done(): return }
			defer func(){ <-sem }()
			c, cancel := context.WithTimeout(ctx, 15*time.Second); r := ProbePlatform(c, p); cancel()
			mu.Lock(); out[p.ID] = r; mu.Unlock()
		}(p)
	}
	wg.Wait(); return out
}

func (m *Manager) PlatformCheck(ctx context.Context, repair bool) (map[string]ProbeResult, error) {
	checks := ProbeAll(ctx)
	m.Mu.Lock(); m.State.LastChecks = checks; m.State.LastPlatformScan = time.Now()
	if e := SaveState(m.Cfg.StatePath, m.State); e != nil { m.Mu.Unlock(); return checks, e }
	m.Mu.Unlock()
	if !repair { return checks, nil }

	var nativeSuspects []string
	m.Mu.Lock()
	for _, p := range Platforms {
		if m.State.RouteMode(p.ID) != "auto" { continue }
		if p.Probe != "" && !nativeProbeCanSelect(p, checks[p.ID]) && m.State.Routes[p.ID] == "native" && m.Cfg.Primary != "" { nativeSuspects = append(nativeSuspects, p.ID) }
	}
	m.Mu.Unlock()
	if len(nativeSuspects) > 0 {
		confirmed := probeSelected(ctx, nativeSuspects); changed := false
		m.Mu.Lock()
		for _, id := range nativeSuspects {
			if r, ok := confirmed[id]; ok {
				checks[id] = r
					p, ok := PlatformByID(id)
					if ok && !nativeProbeCanSelect(p, r) && m.State.Routes[id] == "native" { m.State.Routes[id] = "primary"; changed = true }
			}
		}
		_ = SaveState(m.Cfg.StatePath, m.State); m.Mu.Unlock()
		if changed {
			if _, e := m.ApplyRouteChange(true); e != nil { return checks, e }
			time.Sleep(1500*time.Millisecond)
			for id, r := range probeSelected(ctx, nativeSuspects) { checks[id] = r }
		}
	}

	// Do not switch a platform from primary to backup (or vice versa) on one
	// semantic probe failure. Recheck the current route once first; only a
	// confirmed second failure is eligible for alternate-DNS repair. This keeps
	// transient HTTP/CDN/WAF errors from causing route churn.
	var routeSuspects []string
	m.Mu.Lock()
	for _, p := range Platforms {
		if m.State.RouteMode(p.ID) != "auto" { continue }
		if p.Probe == "" || checks[p.ID].Status != "fail" { continue }
		route := m.State.Routes[p.ID]
		if route == "primary" && m.Cfg.Backup != "" { routeSuspects = append(routeSuspects, p.ID) }
		if route == "backup" && m.Cfg.Primary != "" { routeSuspects = append(routeSuspects, p.ID) }
	}
	m.Mu.Unlock()
	if len(routeSuspects) > 0 {
		confirmed := probeSelected(ctx, routeSuspects)
		for _, id := range routeSuspects {
			if r, ok := confirmed[id]; ok { checks[id] = r }
		}
		m.Mu.Lock(); m.State.LastChecks = checks; _ = SaveState(m.Cfg.StatePath, m.State); m.Mu.Unlock()
	}

	original := map[string]string{}; var candidates []string
	m.Mu.Lock()
	for _, p := range Platforms {
		if m.State.RouteMode(p.ID) != "auto" { continue }
		if p.Probe == "" || checks[p.ID].Status != "fail" { continue }
		route := m.State.Routes[p.ID]; candidate := ""
		if route == "primary" && m.Cfg.Backup != "" { candidate = "backup" } else if route == "backup" && m.Cfg.Primary != "" { candidate = "primary" }
		if candidate == "" { continue }
		original[p.ID] = route; m.State.Routes[p.ID] = candidate; candidates = append(candidates, p.ID)
	}
	if len(candidates) > 0 { _ = SaveState(m.Cfg.StatePath, m.State) }
	m.Mu.Unlock()
	if len(candidates) > 0 {
		if _, e := m.ApplyRouteChange(true); e != nil { return checks, e }
		time.Sleep(1500*time.Millisecond)
		alts := probeSelected(ctx, candidates); restored := false
		m.Mu.Lock()
		for _, id := range candidates {
			if r, ok := alts[id]; ok { checks[id] = r; if r.Status == "pass" { continue } }
			m.State.Routes[id] = original[id]; restored = true
		}
		_ = SaveState(m.Cfg.StatePath, m.State); m.Mu.Unlock()
		if restored { if _, e := m.ApplyRouteChange(true); e != nil { return checks, e } }
	}
	m.Mu.Lock(); m.State.LastChecks = checks; _ = SaveState(m.Cfg.StatePath, m.State); m.Mu.Unlock()
	return checks, nil
}

func Summary(checks map[string]ProbeResult) string {
	pass, fail, unknown := 0,0,0; var fails []string
	for _, p := range Platforms {
		switch checks[p.ID].Status { case "pass": pass++; case "fail": fail++; fails = append(fails,p.Name); default: unknown++ }
	}
	sort.Strings(fails)
	return fmt.Sprintf("综合解锁：通过 %d / 失败 %d / 未判定 %d\n仍失败：%s", pass, fail, unknown, strings.Join(fails, "、"))
}

func (m *Manager) StatusText() string {
	m.Mu.Lock(); defer m.Mu.Unlock(); counts:=map[string]int{}
	for _, v := range m.State.Routes { counts[v]++ }
	modes:=map[string]int{}; for _, p := range Platforms { modes[m.State.RouteMode(p.ID)]++ }
	return fmt.Sprintf("SmartUnlock\n主DNS健康：%v\n备用DNS健康：%v\n模式：自动 %d / 手动 %d\n路由：原生 %d / 主 %d / 备用 %d / 关闭 %d\n规则更新时间：%s\n最近平台检测：%s", m.State.PrimaryHealthy,m.State.BackupHealthy,modes["auto"],modes["manual"],counts["native"],counts["primary"],counts["backup"],counts["off"],m.State.RulesUpdatedAt.Format(time.RFC3339),m.State.LastPlatformScan.Format(time.RFC3339))
}

func (m *Manager) SetRoute(target, route string) error {
	if route!="native" && route!="primary" && route!="backup" && route!="off" { return fmt.Errorf("invalid route") }
	m.Mu.Lock(); defer m.Mu.Unlock()
	if m.State.RouteModes == nil { m.State.RouteModes = map[string]string{} }
	if target=="streaming" || target=="ai" || target=="all" {
		for _, p:=range Platforms { if target=="all" || p.Category==target { m.State.Routes[p.ID]=route; m.State.RouteModes[p.ID]="manual" } }
	} else {
		if _,ok:=PlatformByID(target); !ok { return fmt.Errorf("unknown platform %s", target) }
		m.State.Routes[target]=route
		m.State.RouteModes[target]="manual"
	}
	return SaveState(m.Cfg.StatePath,m.State)
}

func (m *Manager) SetAuto(target string) error {
	m.Mu.Lock(); defer m.Mu.Unlock()
	if m.State.RouteModes == nil { m.State.RouteModes = map[string]string{} }
	set := func(id string) {
		m.State.RouteModes[id] = "auto"
		route := m.State.Routes[id]
		if route == "native" || route == "primary" || route == "backup" { return }
		if m.Cfg.Primary != "" { m.State.Routes[id] = "primary" } else if m.Cfg.Backup != "" { m.State.Routes[id] = "backup" } else { m.State.Routes[id] = "native" }
	}
	if target=="streaming" || target=="ai" || target=="all" {
		for _, p:=range Platforms { if target=="all" || p.Category==target { set(p.ID) } }
	} else {
		if _,ok:=PlatformByID(target); !ok { return fmt.Errorf("unknown platform %s", target) }
		set(target)
	}
	return SaveState(m.Cfg.StatePath,m.State)
}

func EnsureDirs(cfg Config) error {
	for _,d:=range []string{"/etc/smartdns-unlock","/var/lib/smartdns-unlock",cfg.RuntimeDir,"/var/cache/smartdns","/var/log/smartdns"} { if e:=os.MkdirAll(d,0755); e!=nil { return e } }
	return nil
}
