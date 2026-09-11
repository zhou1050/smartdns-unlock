package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const backupRoot = "/etc/smartdns-unlock/backups"

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil { return err }
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil { return err }
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil { return err }
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil { return cpErr }
	return closeErr
}

func marker(path string) bool { _, err := os.Stat(path); return err == nil }
func readTrim(path string) string { b, err := os.ReadFile(path); if err != nil { return "" }; return strings.TrimSpace(string(b)) }
func runSystemctl(args ...string) { _ = exec.Command("systemctl", args...).Run() }

func restoreSystemDNS() error {
	d := filepath.Join(backupRoot, "system-dns-original")
	link := readTrim(filepath.Join(d, "resolv.link"))
	backup := filepath.Join(d, "resolv.conf")
	var warning error

	switch {
	case link != "":
		_ = os.Remove("/etc/resolv.conf")
		if err := os.Symlink(link, "/etc/resolv.conf"); err != nil { return fmt.Errorf("恢复 resolv.conf 符号链接: %w", err) }
	case marker(backup):
		_ = os.Remove("/etc/resolv.conf")
		if err := copyFile(backup, "/etc/resolv.conf", 0644); err != nil { return fmt.Errorf("恢复 resolv.conf: %w", err) }
	default:
		// Never leave a managed 127.0.0.1 resolver behind after stopping SmartUnlock.
		if b, err := os.ReadFile("/etc/resolv.conf"); err == nil && strings.Contains(string(b), "Managed by smartunlock") {
			_ = os.WriteFile("/etc/resolv.conf", []byte("# SmartUnlock backup missing; safe fallback\nnameserver 1.1.1.1\nnameserver 8.8.8.8\noptions timeout:2 attempts:2\n"), 0644)
			warning = fmt.Errorf("未找到安装前系统 DNS 备份，已改用 1.1.1.1 / 8.8.8.8 防止卸载后断网")
		} else {
			warning = fmt.Errorf("未找到安装前系统 DNS 备份，已保留当前 /etc/resolv.conf")
		}
	}

	if marker(filepath.Join(d, "resolved.enabled")) { runSystemctl("enable", "systemd-resolved.service") }
	if marker(filepath.Join(d, "resolved.active")) { runSystemctl("start", "systemd-resolved.service") }
	return warning
}

func restoreOrRemoveSmartDNS(cfg Config) error {
	d := filepath.Join(backupRoot, "smartdns-original")
	preexisting := marker(filepath.Join(d, "preexisting"))
	installedByUs := marker(filepath.Join(d, "installed-by-smartunlock"))

	if preexisting || installedByUs { runSystemctl("disable", "--now", "smartdns.service") }

	if preexisting {
		if p := readTrim(filepath.Join(d, "binary.path")); p != "" && marker(filepath.Join(d, "smartdns.bin")) {
			if err := copyFile(filepath.Join(d, "smartdns.bin"), p, 0755); err != nil { return fmt.Errorf("恢复原 SmartDNS 二进制: %w", err) }
		}
		if marker(filepath.Join(d, "config.existed")) && marker(filepath.Join(d, "smartdns.conf")) {
			if err := copyFile(filepath.Join(d, "smartdns.conf"), cfg.SmartDNSConf, 0640); err != nil { return fmt.Errorf("恢复原 SmartDNS 配置: %w", err) }
		} else if b, err := os.ReadFile(cfg.SmartDNSConf); err == nil && strings.HasPrefix(string(b), "# Managed by smartunlock") {
			_ = os.Remove(cfg.SmartDNSConf)
		}
		for _, pair := range [][2]string{{filepath.Join(d, "default.smartdns"), "/etc/default/smartdns"}, {filepath.Join(d, "init.smartdns"), "/etc/init.d/smartdns"}} {
			if marker(pair[0]) {
				mode := os.FileMode(0640); if strings.Contains(pair[1], "/init.d/") { mode = 0755 }
				_ = copyFile(pair[0], pair[1], mode)
			}
		}
		if src := filepath.Join(d, "smartdns.service"); marker(src) {
			if dst := readTrim(filepath.Join(d, "service.path")); dst != "" { _ = copyFile(src, dst, 0644) }
		}
		runSystemctl("daemon-reload")
		if marker(filepath.Join(d, "service.enabled")) { runSystemctl("enable", "smartdns.service") }
		if marker(filepath.Join(d, "service.active")) { runSystemctl("start", "smartdns.service") }
		return nil
	}

	if installedByUs {
		if p := readTrim(filepath.Join(d, "binary.path")); p != "" { _ = os.Remove(p) } else { _ = os.Remove("/usr/sbin/smartdns") }
		_ = os.Remove("/etc/default/smartdns")
		_ = os.Remove("/etc/init.d/smartdns")
		if p := readTrim(filepath.Join(d, "service.path")); p != "" { _ = os.Remove(p) }
		_ = os.Remove("/lib/systemd/system/smartdns.service")
		_ = os.Remove("/usr/lib/systemd/system/smartdns.service")
		_ = os.Remove(cfg.SmartDNSConf)
		_ = os.Remove("/etc/smartdns/smartdns.conf")
		runSystemctl("daemon-reload")
		return nil
	}

	legacy := "/etc/smartdns/smartdns.conf.before-smartunlock"
	if marker(legacy) {
		if err := copyFile(legacy, cfg.SmartDNSConf, 0640); err != nil { return fmt.Errorf("恢复旧版 SmartDNS 配置备份: %w", err) }
		return nil
	}
	return fmt.Errorf("未找到 SmartDNS 安装来源备份，为避免误删，SmartDNS 程序本身已保留")
}

// Uninstall removes SmartUnlock and restores the DNS/SmartDNS state captured by install.sh.
// It is intentionally conservative when a backup is missing.
func Uninstall(cfg Config) []error {
	var warnings []error
	installedByUs := marker(filepath.Join(backupRoot, "smartdns-original", "installed-by-smartunlock"))
	runSystemctl("disable", "--now", cfg.ServiceName)
	if err := restoreSystemDNS(); err != nil { warnings = append(warnings, err) }
	if err := restoreOrRemoveSmartDNS(cfg); err != nil { warnings = append(warnings, err) }
	_ = os.Remove("/etc/systemd/system/smartunlock.service")
	runSystemctl("daemon-reload")
	_ = os.RemoveAll(cfg.RuntimeDir)
	_ = os.RemoveAll(filepath.Dir(cfg.StatePath))
	if installedByUs {
		_ = os.RemoveAll("/var/cache/smartdns")
		_ = os.RemoveAll("/var/log/smartdns")
	}
	_ = os.RemoveAll("/etc/smartdns-unlock")
	_ = os.Remove("/usr/local/bin/smartunlock")
	return warnings
}
