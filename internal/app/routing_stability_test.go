package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnlockRoutesBypassCacheAndExpiredAnswers(t *testing.T) {
	d := t.TempDir()
	cfg := DefaultConfig()
	cfg.RuntimeDir = filepath.Join(d, "run")
	cfg.SmartDNSConf = filepath.Join(d, "smartdns.conf")
	cfg.Primary = "1.1.1.1"
	cfg.PrimaryProto = "udp"

	s := NewState()
	s.PrimaryHealthy = true
	s.Routes["openai"] = "primary"
	rules := RulesFile{Version: 1, Rules: map[string][]string{"openai": {"chatgpt.com", "openai.com"}}}

	if err := Render(cfg, s, rules); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.RuntimeDir, "platforms.conf"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(b)
	for _, want := range []string{"unlock_primary", "-no-cache", "-no-serve-expired"} {
		if !strings.Contains(line, want) {
			t.Fatalf("unlock route missing %q: %s", want, line)
		}
	}
}
