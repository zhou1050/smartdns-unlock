package app

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Repository        string
	GitHubToken       string
	PrimaryProto      string
	Primary           string
	BackupProto       string
	Backup            string
	CheckInterval     time.Duration
	RuleUpdateTime    string
	PlatformCheckTime string
	TGReportTime      string
	TGBotToken        string
	TGChatID          string
	ConfigPath        string
	StatePath         string
	RulesPath         string
	RuntimeDir        string
	SmartDNSConf      string
	SmartDNSBin       string
	ServiceName       string
	NativeAutoDetect  bool
}

func DefaultConfig() Config {
	return Config{
		Repository: "zhou1050/smartdns-unlock", CheckInterval: 5 * time.Minute,
		RuleUpdateTime: "05:17", PlatformCheckTime: "06:17", TGReportTime: "09:00",
		ConfigPath: "/etc/smartdns-unlock/config.env", StatePath: "/var/lib/smartdns-unlock/state.json",
		RulesPath: "/var/lib/smartdns-unlock/rules.json", RuntimeDir: "/run/smartdns-unlock",
		SmartDNSConf: "/etc/smartdns/smartdns.conf", SmartDNSBin: "smartdns", ServiceName: "smartunlock.service",
		NativeAutoDetect: true,
	}
}

func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	if path != "" {
		c.ConfigPath = path
	}
	f, err := os.Open(c.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	defer f.Close()
	m := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), "\"'")
		m[strings.TrimSpace(k)] = v
	}
	if err := s.Err(); err != nil {
		return c, err
	}
	set := func(k string, p *string) {
		if v, ok := m[k]; ok {
			*p = v
		}
	}
	set("SMARTUNLOCK_REPOSITORY", &c.Repository)
	set("GITHUB_TOKEN", &c.GitHubToken)
	set("UNLOCK_PRIMARY_PROTO", &c.PrimaryProto)
	set("UNLOCK_PRIMARY", &c.Primary)
	set("UNLOCK_BACKUP_PROTO", &c.BackupProto)
	set("UNLOCK_BACKUP", &c.Backup)
	set("RULE_UPDATE_TIME", &c.RuleUpdateTime)
	set("PLATFORM_CHECK_TIME", &c.PlatformCheckTime)
	set("TG_REPORT_TIME", &c.TGReportTime)
	set("TG_BOT_TOKEN", &c.TGBotToken)
	set("TG_CHAT_ID", &c.TGChatID)
	set("SMARTDNS_BIN", &c.SmartDNSBin)
	if v := m["CHECK_INTERVAL"]; v != "" {
		if d, e := time.ParseDuration(v); e == nil {
			c.CheckInterval = d
		}
	}
	if v := m["AUTO_NATIVE_DETECT"]; v != "" {
		b, e := strconv.ParseBool(v)
		if e == nil {
			c.NativeAutoDetect = b
		}
	}
	if c.PrimaryProto == "" {
		c.PrimaryProto = DetectProto(c.Primary)
	}
	if c.BackupProto == "" {
		c.BackupProto = DetectProto(c.Backup)
	}
	return c, nil
}

func DetectProto(endpoint string) string {
	e := strings.ToLower(endpoint)
	switch {
	case strings.HasPrefix(e, "https://"):
		return "doh"
	case strings.HasPrefix(e, "tls://") || strings.HasSuffix(e, ":853"):
		return "dot"
	case strings.HasPrefix(e, "tcp://"):
		return "tcp"
	case strings.HasPrefix(e, "quic://"):
		return "doq"
	case strings.HasPrefix(e, "h3://"):
		return "doh3"
	case endpoint != "":
		return "udp"
	default:
		return ""
	}
}

func (c Config) Validate() error {
	for _, t := range []string{c.RuleUpdateTime, c.PlatformCheckTime, c.TGReportTime} {
		if _, err := time.Parse("15:04", t); err != nil {
			return fmt.Errorf("invalid time %q", t)
		}
	}
	if c.CheckInterval < time.Minute {
		return fmt.Errorf("CHECK_INTERVAL must be >=1m")
	}
	return nil
}
