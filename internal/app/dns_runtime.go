package app

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const localProbeDNSAddr = "127.0.0.1:53"

const managedResolvConf = `# Managed by smartunlock
nameserver 127.0.0.1
nameserver ::1
options timeout:2 attempts:2
`

type probeDNSContextKey struct{}

func withProbeDNS(ctx context.Context, addr string) context.Context {
	if addr == "" {
		return ctx
	}
	return context.WithValue(ctx, probeDNSContextKey{}, addr)
}

func probeDNSFromContext(ctx context.Context) string {
	v, _ := ctx.Value(probeDNSContextKey{}).(string)
	return v
}

// probeHTTPClient deliberately ignores process proxy variables and, when a
// probe DNS address is attached to the context, resolves every probe hostname
// through that DNS listener. This keeps post-install platform checks bound to
// SmartDNS even if DHCP temporarily overwrites /etc/resolv.conf.
func probeHTTPClient(ctx context.Context) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	if addr := probeDNSFromContext(ctx); addr != "" {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(dctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(dctx, network, addr)
			},
		}
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Resolver: resolver}
		tr.DialContext = d.DialContext
	}
	return &http.Client{Timeout: 12 * time.Second, Transport: tr}
}

// ensureManagedResolvConf restores the resolver file SmartUnlock owns after
// installation. It only rewrites on drift, so a normal steady-state daemon
// does not churn /etc/resolv.conf. Atomic rename also replaces a DHCP-created
// symlink/file without exposing a partially written resolver configuration.
func ensureManagedResolvConf(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err == nil && string(b) == managedResolvConf {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".smartunlock-resolv-*")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.WriteString(managedResolvConf); err != nil {
		_ = f.Close()
		return false, err
	}
	if err = f.Chmod(0644); err != nil {
		_ = f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}
