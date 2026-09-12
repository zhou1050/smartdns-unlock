package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func nextClock(now time.Time, hhmm string) time.Time {
	t, _ := time.Parse("15:04", hhmm)
	n := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !n.After(now) {
		n = n.Add(24 * time.Hour)
	}
	return n
}

func resetTimer(t *time.Timer, d time.Duration) {
	if d < 0 {
		d = 0
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func RunDaemon(ctx context.Context, cfg Config) error {
	if err := EnsureDirs(cfg); err != nil {
		return err
	}
	m, err := NewManager(cfg)
	if err != nil {
		return err
	}
	m.OwnDNS = true
	if len(m.Rules.Rules) == 0 {
		c, cancel := context.WithTimeout(ctx, 90*time.Second)
		err = m.SyncRules(c)
		cancel()
		if err != nil {
			return fmt.Errorf("initial rule sync: %w", err)
		}
	}
	if !m.State.Initialized {
		log.Printf("state not initialized; conservative primary routing")
		for _, p := range Platforms {
			m.State.Routes[p.ID] = "primary"
		}
		m.State.Initialized = true
		_ = SaveState(cfg.StatePath, m.State)
	}
	if err := m.Apply(false); err != nil {
		return err
	}
	if err := m.DNS.Start(); err != nil {
		return err
	}
	defer m.DNS.Stop()
	_, _, _ = m.HealthCheck(ctx)

	sig := make(chan os.Signal, 16)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sig)
	health := time.NewTicker(cfg.CheckInterval)
	defer health.Stop()
	ruleTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.RuleUpdateTime)))
	defer ruleTimer.Stop()
	platformTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.PlatformCheckTime)))
	defer platformTimer.Stop()
	reportTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.TGReportTime)))
	defer reportTimer.Stop()

	// CLI operations can arrive back-to-back (update/on/off/on). Restarting
	// SmartDNS for every HUP creates a short DNS outage. Coalesce the whole
	// burst into one trailing-edge reload after a quiet window long enough to
	// cover sequential CLI commands and disk/state updates.
	var reloadTimer *time.Timer
	var reloadC <-chan time.Time
	queueReload := func() {
		const debounce = 2 * time.Second
		if reloadTimer == nil {
			reloadTimer = time.NewTimer(debounce)
		} else {
			resetTimer(reloadTimer, debounce)
		}
		reloadC = reloadTimer.C
	}
	defer func() {
		if reloadTimer != nil {
			reloadTimer.Stop()
		}
	}()

	reload := func() {
		nc, e := LoadConfig(cfg.ConfigPath)
		if e != nil {
			log.Printf("reload config: %v", e)
			return
		}
		if e = nc.Validate(); e != nil {
			log.Printf("reload config rejected: %v", e)
			return
		}
		cfg = nc
		m.Cfg = nc
		m.DNS.SetConfig(nc)
		if st, e := LoadState(cfg.StatePath); e == nil {
			m.Mu.Lock()
			m.State = st
			m.Mu.Unlock()
		}
		if rr, e := LoadRules(cfg.RulesPath); e == nil {
			m.Mu.Lock()
			m.Rules = rr
			m.Mu.Unlock()
		}
		health.Reset(cfg.CheckInterval)
		resetTimer(ruleTimer, time.Until(nextClock(time.Now(), cfg.RuleUpdateTime)))
		resetTimer(platformTimer, time.Until(nextClock(time.Now(), cfg.PlatformCheckTime)))
		resetTimer(reportTimer, time.Until(nextClock(time.Now(), cfg.TGReportTime)))
		if e := m.Apply(true); e != nil {
			log.Printf("reload apply: %v", e)
		} else {
			if e := writeReloadAck(cfg); e != nil {
				log.Printf("reload acknowledgement: %v", e)
			}
			log.Printf("configuration reloaded and schedules reset")
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-sig:
			if s == syscall.SIGHUP {
				queueReload()
				continue
			}
			return nil
		case <-reloadC:
			reloadC = nil
			reload()
		case <-health.C:
			c, cancel := context.WithTimeout(ctx, 12*time.Second)
			_, _, _ = m.HealthCheck(c)
			cancel()
		case <-ruleTimer.C:
			c, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if e := m.SyncRules(c); e != nil {
				log.Printf("rule update: %v", e)
			} else {
				_ = m.DNS.Restart()
			}
			cancel()
			ruleTimer.Reset(time.Until(nextClock(time.Now(), cfg.RuleUpdateTime)))
		case <-platformTimer.C:
			c, cancel := context.WithTimeout(ctx, 12*time.Minute)
			checks, e := m.PlatformCheck(c, true)
			if e != nil {
				log.Printf("platform check: %v", e)
			} else {
				msg := Summary(checks)
				log.Print(msg)
				_ = NotifyTelegram(c, cfg, "✅ SmartDNS 每日"+msg)
			}
			cancel()
			platformTimer.Reset(time.Until(nextClock(time.Now(), cfg.PlatformCheckTime)))
		case <-reportTimer.C:
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
			_ = NotifyTelegram(c, cfg, "📊 "+m.StatusText())
			cancel()
			reportTimer.Reset(time.Until(nextClock(time.Now(), cfg.TGReportTime)))
		}
	}
}
