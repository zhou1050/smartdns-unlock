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

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	health := time.NewTicker(cfg.CheckInterval)
	defer health.Stop()
	ruleTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.RuleUpdateTime)))
	defer ruleTimer.Stop()
	platformTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.PlatformCheckTime)))
	defer platformTimer.Stop()
	reportTimer := time.NewTimer(time.Until(nextClock(time.Now(), cfg.TGReportTime)))
	defer reportTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-sig:
			if s == syscall.SIGHUP {
				if nc, e := LoadConfig(cfg.ConfigPath); e == nil {
					cfg = nc
					m.Cfg = nc
				}
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
				_ = m.Apply(true)
				continue
			}
			return nil
		case <-health.C:
			c, cancel := context.WithTimeout(ctx, 20*time.Second)
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
