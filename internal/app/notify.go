package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func telegramServerIdentity() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown"
	}
	host = strings.TrimSpace(host)

	// Pick the IPv4 that the kernel would use for the default route. UDP Dial
	// does not need a successful DNS exchange; it is only used to ask the kernel
	// which local source address would be selected.
	if c, err := net.DialTimeout("udp4", "1.1.1.1:53", 800*time.Millisecond); err == nil {
		if a, ok := c.LocalAddr().(*net.UDPAddr); ok && a.IP != nil && !a.IP.IsLoopback() {
			ip := a.IP.String()
			_ = c.Close()
			return fmt.Sprintf("%s (%s)", host, ip)
		}
		_ = c.Close()
	}

	// Fallback for hosts without a usable default route yet.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() {
				return fmt.Sprintf("%s (%s)", host, ip.String())
			}
		}
	}
	return host
}

func NotifyTelegram(ctx context.Context, cfg Config, msg string) error {
	if cfg.TGBotToken == "" || cfg.TGChatID == "" {
		return nil
	}
	msg = "🖥 服务器：" + telegramServerIdentity() + "\n" + msg
	v := url.Values{}
	v.Set("chat_id", cfg.TGChatID)
	v.Set("text", msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+cfg.TGBotToken+"/sendMessage", strings.NewReader(v.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl := http.Client{Timeout: 15 * time.Second}
	r, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return nil
}
