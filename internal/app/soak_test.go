package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type controllableDNS struct {
	mu   sync.Mutex
	addr string
	pc   net.PacketConn
	done chan struct{}
}

func newControllableDNS(t *testing.T) *controllableDNS {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	d := &controllableDNS{addr: pc.LocalAddr().String(), pc: pc, done: make(chan struct{})}
	d.serve(pc, d.done)
	return d
}

func (d *controllableDNS) serve(pc net.PacketConn, done chan struct{}) {
	go func() {
		buf := make([]byte, 2048)
		for {
			_ = pc.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				select { case <-done: return; default: continue }
			}
			if n < 12 { continue }
			q := append([]byte(nil), buf[:n]...)
			binary.BigEndian.PutUint16(q[2:4], 0x8180)
			binary.BigEndian.PutUint16(q[6:8], 1)
			ans := []byte{0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x3c, 0x00, 0x04, 0x5d, 0xb8, 0xd8, 0x22}
			_, _ = pc.WriteTo(append(q, ans...), addr)
		}
	}()
}

func (d *controllableDNS) Stop() {
	d.mu.Lock(); defer d.mu.Unlock()
	if d.pc == nil { return }
	close(d.done); _ = d.pc.Close(); d.pc = nil
}
func (d *controllableDNS) Start(t *testing.T) {
	t.Helper(); d.mu.Lock(); defer d.mu.Unlock()
	if d.pc != nil { return }
	pc, err := net.ListenPacket("udp", d.addr)
	if err != nil { t.Fatalf("restart DNS %s: %v", d.addr, err) }
	d.pc = pc; d.done = make(chan struct{}); d.serve(pc, d.done)
}

func readRSSKB() int64 {
	b, err := os.ReadFile("/proc/self/status"); if err != nil { return 0 }
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmRSS:") {
			f := strings.Fields(ln); if len(f) >= 2 { n, _ := strconv.ParseInt(f[1], 10, 64); return n }
		}
	}
	return 0
}

func waitHealth(t *testing.T, path string, primary, backup bool, timeout time.Duration) State {
	t.Helper(); deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			var s State
			if json.Unmarshal(b, &s) == nil && s.PrimaryHealthy == primary && s.BackupHealthy == backup { return s }
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("health state did not become primary=%v backup=%v", primary, backup)
	return State{}
}

func mustContain(t *testing.T, path, want string) {
	t.Helper(); b, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	if !strings.Contains(string(b), want) { t.Fatalf("%s missing %q:\n%s", path, want, string(b)) }
}

func mustRouteContain(t *testing.T, path, routeName, want string) {
	t.Helper(); b, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "domain-set:"+routeName) {
			if !strings.Contains(line, want) { t.Fatalf("route %s missing %q: %s", routeName, want, line) }
			return
		}
	}
	t.Fatalf("route %s not found in %s", routeName, path)
}

func TestDaemonSoak(t *testing.T) {
	if os.Getenv("SMARTUNLOCK_SOAK") != "1" { t.Skip("set SMARTUNLOCK_SOAK=1") }
	start := time.Now()
	targetDuration := 10 * time.Minute
	for _, p := range []string{"/etc/smartdns-unlock", "/var/lib/smartdns-unlock", "/run/smartdns-unlock", "/var/cache/smartdns", "/var/log/smartdns", "/etc/smartdns"} { _ = os.RemoveAll(p) }
	defer func(){ for _, p := range []string{"/etc/smartdns-unlock", "/var/lib/smartdns-unlock", "/run/smartdns-unlock", "/var/cache/smartdns", "/var/log/smartdns", "/etc/smartdns"} { _ = os.RemoveAll(p) } }()

	primary := newControllableDNS(t); defer primary.Stop()
	backup := newControllableDNS(t); defer backup.Stop()
	d := t.TempDir(); fakeLog := filepath.Join(d, "fake-smartdns.log"); fake := filepath.Join(d, "smartdns")
	script := fmt.Sprintf("#!/bin/sh\necho start >> %q\ntrap 'echo stop >> %q; exit 0' TERM INT\nwhile :; do sleep 1; done\n", fakeLog, fakeLog)
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil { t.Fatal(err) }

	cfg := DefaultConfig()
	cfg.Primary, cfg.PrimaryProto = "127.0.0.1:53001", "udp"
	cfg.Backup, cfg.BackupProto = "127.0.0.1:53002", "udp"
	cfg.HealthPrimaryAddr, cfg.HealthBackupAddr = primary.addr, backup.addr
	cfg.SmartDNSBin = fake
	cfg.CheckInterval = time.Second // soak only; production validation remains >=1m
	if err := EnsureDirs(cfg); err != nil { t.Fatal(err) }
	rules := RulesFile{Version:1, UpdatedAt:time.Now(), Rules:map[string][]string{"netflix":{"netflix.com"}, "openai":{"openai.com","chatgpt.com"}, "gemini":{"gemini.google.com"}}}
	if err := SaveRules(cfg.RulesPath, rules); err != nil { t.Fatal(err) }
	s := NewState(); s.Initialized = true; s.Routes["netflix"] = "primary"; s.Routes["openai"] = "backup"; s.Routes["gemini"] = "backup"; s.RouteModes["gemini"] = "manual"
	if err := SaveState(cfg.StatePath, s); err != nil { t.Fatal(err) }
	configText := fmt.Sprintf("UNLOCK_PRIMARY_PROTO=udp\nUNLOCK_PRIMARY=127.0.0.1:53001\nUNLOCK_BACKUP_PROTO=udp\nUNLOCK_BACKUP=127.0.0.1:53002\nCHECK_INTERVAL=1m\nRULE_UPDATE_TIME=23:51\nPLATFORM_CHECK_TIME=23:52\nTG_REPORT_TIME=23:53\nSMARTDNS_BIN=%s\nHEALTH_PRIMARY_ADDR=%s\nHEALTH_BACKUP_ADDR=%s\nAUTO_NATIVE_DETECT=true\n", fake, primary.addr, backup.addr)
	if err := os.WriteFile(cfg.ConfigPath, []byte(configText), 0600); err != nil { t.Fatal(err) }

	ctx, cancel := context.WithCancel(context.Background()); defer cancel()
	errCh := make(chan error, 1)
	go func(){ errCh <- RunDaemon(ctx, cfg) }()
	waitHealth(t, cfg.StatePath, true, true, 8*time.Second)
	t.Logf("[+%s] daemon started, both upstream groups healthy", time.Since(start).Round(time.Second))

	baseG := runtime.NumGoroutine(); baseRSS := readRSSKB(); maxG := baseG; maxRSS := baseRSS
	monitorDone := make(chan struct{})
	monitorStopped := make(chan struct{})
	go func(){ defer close(monitorStopped); ticker := time.NewTicker(2*time.Second); defer ticker.Stop(); for { select { case <-monitorDone: return; case <-ticker.C: g:=runtime.NumGoroutine(); r:=readRSSKB(); if g>maxG { maxG=g }; if r>maxRSS { maxRSS=r } } } }()

	time.Sleep(20*time.Second)
	primary.Stop(); waitHealth(t, cfg.StatePath, false, true, 8*time.Second)
	mustContain(t, filepath.Join(cfg.RuntimeDir, "platforms.conf"), "su_netflix")
	mustContain(t, filepath.Join(cfg.RuntimeDir, "platforms.conf"), "unlock_backup")
	t.Logf("[+%s] injected primary DNS failure -> primary routes failed over", time.Since(start).Round(time.Second))

	time.Sleep(15*time.Second)
	primary.Start(t); waitHealth(t, cfg.StatePath, true, true, 8*time.Second)
	t.Logf("[+%s] primary DNS recovered", time.Since(start).Round(time.Second))

	time.Sleep(15*time.Second)
	backup.Stop(); waitHealth(t, cfg.StatePath, true, false, 8*time.Second)
	mustContain(t, filepath.Join(cfg.RuntimeDir, "platforms.conf"), "su_openai")
	mustContain(t, filepath.Join(cfg.RuntimeDir, "platforms.conf"), "unlock_primary")
	mustRouteContain(t, filepath.Join(cfg.RuntimeDir, "platforms.conf"), "su_gemini", "unlock_backup")
	t.Logf("[+%s] injected backup DNS failure -> backup routes failed back", time.Since(start).Round(time.Second))

	time.Sleep(15*time.Second)
	backup.Start(t); waitHealth(t, cfg.StatePath, true, true, 8*time.Second)
	t.Logf("[+%s] backup DNS recovered", time.Since(start).Round(time.Second))

	// Hold a long stable window so the entire fault-injection soak lasts ten minutes.
	if wait := time.Until(start.Add(targetDuration - 30*time.Second)); wait > 0 { time.Sleep(wait) }
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil { t.Fatal(err) }
	time.Sleep(5*time.Second)
	t.Logf("[+%s] SIGHUP reload exercised", time.Since(start).Round(time.Second))

	// Continue until the ten-minute mark to catch delayed post-reload failures.
	if wait := time.Until(start.Add(targetDuration)); wait > 0 { time.Sleep(wait) }
	close(monitorDone)
	<-monitorStopped
	cancel()
	select { case err := <-errCh: if err != nil { t.Fatalf("daemon exit: %v", err) }; case <-time.After(5*time.Second): t.Fatal("daemon did not stop cleanly") }

	logBytes, _ := os.ReadFile(fakeLog); starts := strings.Count(string(logBytes), "start")
	if starts > 10 { t.Fatalf("restart storm detected: fake SmartDNS started %d times\n%s", starts, string(logBytes)) }
	if maxG > baseG+20 { t.Fatalf("goroutine growth suspicious: base=%d max=%d", baseG, maxG) }
	if baseRSS > 0 && maxRSS > baseRSS+48*1024 { t.Fatalf("RSS growth suspicious: base=%dKB max=%dKB", baseRSS, maxRSS) }
	t.Logf("[+%s] PASS starts=%d goroutines base/max=%d/%d RSS base/max=%d/%dKB", time.Since(start).Round(time.Second), starts, baseG, maxG, baseRSS, maxRSS)
}
