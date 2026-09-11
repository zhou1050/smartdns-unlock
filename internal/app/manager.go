package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	if e != nil {
		return nil, e
	}
	r, e := LoadRules(cfg.RulesPath)
	if e != nil {
		r = RulesFile{Version: 1, Rules: map[string][]string{}}
	}
	return &Manager{Cfg: cfg, State: s, Rules: r, DNS: NewSmartDNSProcess(cfg)}, nil
}

func (m *Manager) SyncRules(ctx context.Context) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	r, e := SyncRules(ctx, m.Cfg)
	if e != nil {
		return e
	}
	m.Rules = r
	m.State.RulesUpdatedAt = r.UpdatedAt
	if e = SaveState(m.Cfg.StatePath, m.State); e != nil {
		return e
	}
	return Render(m.Cfg, m.State, m.Rules)
}

func (m *Manager) restartDNS() error {
	if m.OwnDNS {
		return m.DNS.Restart()
	}
	return exec.Command("systemctl", "kill", "-s", "HUP", m.Cfg.ServiceName).Run()
}

func (m *Manager) Apply(restart bool) error {
	m.Mu.Lock()
	if len(m.Rules.Rules) == 0 {
		r, e := LoadRules(m.Cfg.RulesPath)
		if e != nil {
			m.Mu.Unlock()
			return e
		}
		m.Rules = r
	}
	if e := Render(m.Cfg, m.State, m.Rules); e != nil {
		m.Mu.Unlock()
		return e
	}
	m.Mu.Unlock()
	if restart {
		return m.restartDNS()
	}
	return nil
}

func (m *Manager) NativeScan(ctx context.Context) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	checks := ProbeAll(ctx)
	if m.State.Routes == nil {
		m.State.Routes = map[string]string{}
	}
	for _, p := range Platforms {
		r := checks[p.ID]
		if !m.Cfg.NativeAutoDetect {
			m.State.Routes[p.ID] = "primary"
			continue
		}
		if p.Probe != "" && r.Status == "pass" {
			m.State.Routes[p.ID] = "native"
		} else {
			m.State.Routes[p.ID] = "primary"
		}
	}
	m.State.LastChecks = checks
	m.State.Initialized = true
	m.State.LastPlatformScan = time.Now()
	return SaveState(m.Cfg.StatePath, m.State)
}

func (m *Manager) HealthCheck(ctx context.Context) (bool, bool, error) {
	p := CheckEndpoint(ctx, m.Cfg.PrimaryProto, m.Cfg.Primary)
	b := false
	if m.Cfg.Backup != "" {
		b = CheckEndpoint(ctx, m.Cfg.BackupProto, m.Cfg.Backup)
	}
	m.Mu.Lock()
	oldp, oldb := m.State.PrimaryHealthy, m.State.BackupHealthy
	m.State.PrimaryHealthy = p
	m.State.BackupHealthy = b
	e := SaveState(m.Cfg.StatePath, m.State)
	changed := oldp != p || oldb != b
	m.Mu.Unlock()
	if e != nil {
		return p, b, e
	}
	if changed {
		_ = m.Apply(true)
		_ = NotifyTelegram(ctx, m.Cfg, fmt.Sprintf("SmartDNS 上游健康变化：主=%v 备用=%v", p, b))
	}
	return p, b, nil
}

func (m *Manager) PlatformCheck(ctx context.Context, repair bool) (map[string]ProbeResult, error) {
	checks := ProbeAll(ctx)
	m.Mu.Lock()
	m.State.LastChecks = checks
	m.State.LastPlatformScan = time.Now()
	changed := false
	for _, p := range Platforms {
		r := checks[p.ID]
		if p.Probe == "" || r.Status != "fail" {
			continue
		}
		route := m.State.Routes[p.ID]
		if repair && route == "native" && m.Cfg.Primary != "" {
			m.State.Routes[p.ID] = "primary"
			changed = true
		}
	}
	if e := SaveState(m.Cfg.StatePath, m.State); e != nil {
		m.Mu.Unlock()
		return checks, e
	}
	m.Mu.Unlock()
	if changed {
		if e := m.Apply(true); e != nil {
			return checks, e
		}
		time.Sleep(2 * time.Second)
		checks = ProbeAll(ctx)
	}
	if repair && m.Cfg.Backup != "" {
		for _, p := range Platforms {
			if p.Probe == "" || checks[p.ID].Status != "fail" {
				continue
			}
			m.Mu.Lock()
			route := m.State.Routes[p.ID]
			m.Mu.Unlock()
			if route != "primary" && route != "backup" {
				continue
			}
			candidate := "backup"
			if route == "backup" {
				candidate = "primary"
			}
			m.Mu.Lock()
			m.State.Routes[p.ID] = candidate
			_ = SaveState(m.Cfg.StatePath, m.State)
			m.Mu.Unlock()
			_ = m.Apply(true)
			time.Sleep(1200 * time.Millisecond)
			pp, _ := PlatformByID(p.ID)
			c, cancel := context.WithTimeout(ctx, 15*time.Second)
			pr := ProbePlatform(c, pp)
			cancel()
			if pr.Status == "pass" {
				checks[p.ID] = pr
				continue
			}
			m.Mu.Lock()
			m.State.Routes[p.ID] = route
			_ = SaveState(m.Cfg.StatePath, m.State)
			m.Mu.Unlock()
			_ = m.Apply(true)
		}
	}
	m.Mu.Lock()
	m.State.LastChecks = checks
	_ = SaveState(m.Cfg.StatePath, m.State)
	m.Mu.Unlock()
	return checks, nil
}

func Summary(checks map[string]ProbeResult) string {
	pass, fail, unknown := 0, 0, 0
	var fails []string
	for _, p := range Platforms {
		switch checks[p.ID].Status {
		case "pass":
			pass++
		case "fail":
			fail++
			fails = append(fails, p.Name)
		default:
			unknown++
		}
	}
	sort.Strings(fails)
	return fmt.Sprintf("综合解锁：通过 %d / 失败 %d / 未判定 %d\n仍失败：%s", pass, fail, unknown, strings.Join(fails, "、"))
}

func (m *Manager) StatusText() string {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	counts := map[string]int{}
	for _, v := range m.State.Routes {
		counts[v]++
	}
	return fmt.Sprintf("SmartUnlock\n主DNS健康：%v\n备用DNS健康：%v\n路由：原生 %d / 主 %d / 备用 %d / 关闭 %d\n规则更新时间：%s\n最近平台检测：%s", m.State.PrimaryHealthy, m.State.BackupHealthy, counts["native"], counts["primary"], counts["backup"], counts["off"], m.State.RulesUpdatedAt.Format(time.RFC3339), m.State.LastPlatformScan.Format(time.RFC3339))
}

func (m *Manager) SetRoute(target, route string) error {
	if route != "native" && route != "primary" && route != "backup" && route != "off" {
		return fmt.Errorf("invalid route")
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if target == "streaming" || target == "ai" || target == "all" {
		for _, p := range Platforms {
			if target == "all" || p.Category == target {
				m.State.Routes[p.ID] = route
			}
		}
	} else {
		if _, ok := PlatformByID(target); !ok {
			return fmt.Errorf("unknown platform %s", target)
		}
		m.State.Routes[target] = route
	}
	return SaveState(m.Cfg.StatePath, m.State)
}

func EnsureDirs(cfg Config) error {
	for _, d := range []string{"/etc/smartdns-unlock", "/var/lib/smartdns-unlock", cfg.RuntimeDir, "/var/cache/smartdns", "/var/log/smartdns"} {
		if e := os.MkdirAll(d, 0755); e != nil {
			return e
		}
	}
	return nil
}
