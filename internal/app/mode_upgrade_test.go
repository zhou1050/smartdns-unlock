package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyStateModeMigration(t *testing.T) {
	d := t.TempDir()
	path := filepath.Join(d, "state.json")
	legacy := `{"version":1,"initialized":true,"routes":{"netflix":"backup","gemini":"backup","openai":"off"}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 2 {
		t.Fatalf("version=%d, want 2", s.Version)
	}
	if got := s.RouteMode("netflix"); got != "auto" {
		t.Fatalf("legacy probed route mode=%s, want auto", got)
	}
	if got := s.RouteMode("gemini"); got != "manual" {
		t.Fatalf("legacy unprobed backup mode=%s, want manual", got)
	}
	if got := s.RouteMode("openai"); got != "manual" {
		t.Fatalf("legacy off mode=%s, want manual", got)
	}
}

func TestSetRouteAndAutoMode(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig()
	cfg.StatePath = filepath.Join(d, "state.json")
	cfg.Primary = "primary.example:53"
	cfg.Backup = "backup.example:53"
	m := &Manager{Cfg: cfg, State: NewState()}

	if err := m.SetRoute("gemini", "backup"); err != nil {
		t.Fatal(err)
	}
	if m.State.RouteMode("gemini") != "manual" || m.State.Routes["gemini"] != "backup" {
		t.Fatalf("manual state=%+v", m.State)
	}
	if err := m.SetAuto("gemini"); err != nil {
		t.Fatal(err)
	}
	if m.State.RouteMode("gemini") != "auto" || m.State.Routes["gemini"] != "backup" {
		t.Fatalf("auto should preserve valid current route: %+v", m.State)
	}
	if err := m.SetRoute("gemini", "off"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetAuto("gemini"); err != nil {
		t.Fatal(err)
	}
	if m.State.Routes["gemini"] != "primary" {
		t.Fatalf("auto should reactivate an off route via primary: %+v", m.State)
	}
}

func TestManualRouteDoesNotHealthFailover(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig()
	cfg.RuntimeDir = filepath.Join(d, "run")
	cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary, cfg.PrimaryProto = "primary.example:53", "udp"
	cfg.Backup, cfg.BackupProto = "backup.example:53", "udp"
	s := NewState()
	s.Routes["gemini"] = "backup"
	s.RouteModes["gemini"] = "manual"
	s.PrimaryHealthy = true
	s.BackupHealthy = false
	rules := RulesFile{Version: 1, Rules: map[string][]string{"gemini": {"gemini.google.com"}}}
	if err := Render(cfg, s, rules); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "-nameserver unlock_backup") || strings.Contains(text, "-nameserver unlock_primary") {
		t.Fatalf("manual backup route changed during health failure: %s", text)
	}
}

func TestChecksumFor(t *testing.T) {
	hash := strings.Repeat("a", 64)
	data := []byte(hash + "  dist/smartunlock-linux-amd64\n" + strings.Repeat("b", 64) + "  dist/smartunlock-linux-arm64\n")
	got, err := checksumFor(data, "smartunlock-linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got != hash {
		t.Fatalf("checksum=%q, want %q", got, hash)
	}
}
