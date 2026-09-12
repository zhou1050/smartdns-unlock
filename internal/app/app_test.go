package app

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRenderNativeAndPrimary(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig(); cfg.RuntimeDir = filepath.Join(d, "run"); cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "https://primary.example/dns-query"; cfg.PrimaryProto = "doh"; cfg.Backup = "tls://backup.example:853"; cfg.BackupProto = "dot"
	s := NewState(); s.Routes["netflix"] = "native"; s.Routes["openai"] = "primary"
	r := RulesFile{Version: 1, Rules: map[string][]string{"netflix": {"netflix.com"}, "openai": {"openai.com", "chatgpt.com"}}}
	if err := Render(cfg, s, r); err != nil { t.Fatal(err) }
	b, err := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf")); if err != nil { t.Fatal(err) }
	text := string(b)
	if strings.Contains(text, "su_netflix") { t.Fatalf("native platform rendered: %s", text) }
	if !strings.Contains(text, "su_openai") || !strings.Contains(text, "unlock_primary") { t.Fatalf("primary route missing: %s", text) }
}

func TestPrimaryHealthFallback(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig(); cfg.RuntimeDir = filepath.Join(d, "run"); cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "https://primary.example/dns-query"; cfg.PrimaryProto = "doh"; cfg.Backup = "tls://backup.example:853"; cfg.BackupProto = "dot"
	s := NewState(); s.Routes["openai"] = "primary"; s.PrimaryHealthy = false; s.BackupHealthy = true
	r := RulesFile{Version: 1, Rules: map[string][]string{"openai": {"openai.com"}}}
	if err := Render(cfg, s, r); err != nil { t.Fatal(err) }
	b, _ := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if !strings.Contains(string(b), "unlock_backup") { t.Fatalf("health fallback not rendered: %s", string(b)) }
}

func TestBackupHealthFailback(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig(); cfg.RuntimeDir = filepath.Join(d, "run"); cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "https://primary.example/dns-query"; cfg.PrimaryProto = "doh"; cfg.Backup = "tls://backup.example:853"; cfg.BackupProto = "dot"
	s := NewState(); s.Routes["openai"] = "backup"; s.PrimaryHealthy = true; s.BackupHealthy = false
	r := RulesFile{Version: 1, Rules: map[string][]string{"openai": {"openai.com"}}}
	if err := Render(cfg, s, r); err != nil { t.Fatal(err) }
	b, _ := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if !strings.Contains(string(b), "unlock_primary") { t.Fatalf("backup failback not rendered: %s", string(b)) }
}

func TestAllUnlockUpstreamsDownFallsBackToDefaultDNS(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig(); cfg.RuntimeDir = filepath.Join(d, "run"); cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "https://primary.example/dns-query"; cfg.PrimaryProto = "doh"; cfg.Backup = "https://backup.example/dns-query"; cfg.BackupProto = "doh"
	s := NewState(); s.Routes["openai"] = "primary"; s.PrimaryHealthy = false; s.BackupHealthy = false
	r := RulesFile{Version: 1, Rules: map[string][]string{"openai": {"openai.com"}}}
	if err := Render(cfg, s, r); err != nil { t.Fatal(err) }
	b, _ := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if strings.Contains(string(b), "su_openai") { t.Fatalf("dead unlock route should be omitted for public fallback: %s", string(b)) }
}

func TestOnlyPrimaryDownFallsBackToDefaultDNS(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig(); cfg.RuntimeDir = filepath.Join(d, "run"); cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "https://primary.example/dns-query"; cfg.PrimaryProto = "doh"; cfg.Backup = ""; cfg.BackupProto = ""
	s := NewState(); s.Routes["openai"] = "primary"; s.PrimaryHealthy = false
	r := RulesFile{Version: 1, Rules: map[string][]string{"openai": {"openai.com"}}}
	if err := Render(cfg, s, r); err != nil { t.Fatal(err) }
	b, _ := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if strings.Contains(string(b), "su_openai") { t.Fatalf("dead sole unlock route should be omitted: %s", string(b)) }
}

func startTestDNS(t *testing.T) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			_ = pc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
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
	var once sync.Once
	return pc.LocalAddr().String(), func(){ once.Do(func(){ close(done); _ = pc.Close() }) }
}

func TestCheckDNSListener(t *testing.T) {
	addr, stop := startTestDNS(t); defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second); defer cancel()
	if !CheckDNSListener(ctx, addr) { t.Fatal("working DNS listener reported unhealthy") }
	stop()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond); defer cancel2()
	if CheckDNSListener(ctx2, addr) { t.Fatal("stopped DNS listener reported healthy") }
}

func TestReliableProbeRegistry(t *testing.T) {
	want := []string{
		"netflix", "disney", "youtube", "primevideo", "max", "hulu", "spotify", "tiktok", "dazn", "bbciplayer",
		"paramount", "peacock", "crunchyroll", "abema", "bahamut", "bilibili", "iqiyi", "viu", "tvb", "openai", "claude", "microsoftcopilot",
	}
	var got []string
	for _, p := range Platforms {
		if p.Probe != "" { got = append(got, p.ID) }
	}
	sort.Strings(want); sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("reliable probe registry changed\nwant=%v\n got=%v", want, got)
	}
}

func TestProbeJSONHelpers(t *testing.T) {
	body := `{"Region":{"isAllowed":true,"GeolocatedCountry":"JP"},"nested":{"items":[{"store_region":"SG"}]}}`
	if b, ok := jsonBoolAt(body, "Region", "isAllowed"); !ok || !b { t.Fatal("nested bool parse failed") }
	if got := jsonStringAt(body, "Region", "GeolocatedCountry"); got != "JP" { t.Fatalf("nested string=%q", got) }
	if got := findJSONString(body, "store_region"); got != "SG" { t.Fatalf("recursive string=%q", got) }
}
