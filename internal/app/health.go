package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func endpointHostPort(proto, endpoint string) (string, string, error) {
	e := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(endpoint, "tls://"), "tcp://"), "quic://"), "h3://")
	if proto == "doh" || proto == "doh3" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return "", "", err
		}
		h := u.Hostname()
		p := u.Port()
		if p == "" {
			p = "443"
		}
		return h, p, nil
	}
	h, p, err := net.SplitHostPort(e)
	if err == nil {
		return h, p, nil
	}
	if net.ParseIP(e) != nil || !strings.Contains(e, ":") {
		if proto == "dot" || proto == "doq" {
			return e, "853", nil
		}
		return e, "53", nil
	}
	return "", "", err
}

func CheckEndpoint(ctx context.Context, proto, endpoint string) bool {
	if endpoint == "" {
		return false
	}
	h, p, err := endpointHostPort(proto, endpoint)
	if err != nil {
		return false
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	switch proto {
	case "doh", "doh3":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return false
		}
		cl := http.Client{Timeout: 7 * time.Second}
		r, err := cl.Do(req)
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode > 0
	case "dot":
		c, err := tls.DialWithDialer(&d, "tcp", net.JoinHostPort(h, p), &tls.Config{ServerName: h, MinVersion: tls.VersionTLS12})
		if err != nil {
			return false
		}
		c.Close()
		return true
	case "tcp":
		c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(h, p))
		if err != nil {
			return false
		}
		c.Close()
		return true
	case "udp", "doq":
		c, err := d.DialContext(ctx, "udp", net.JoinHostPort(h, p))
		if err != nil {
			return false
		}
		c.Close()
		return true
	default:
		_ = fmt.Sprintf("")
		return false
	}
}
