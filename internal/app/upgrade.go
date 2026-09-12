package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const installedBinaryPath = "/usr/local/bin/smartunlock"

func downloadUpgradeFile(ctx context.Context, url, token string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "smartunlock-upgrader")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", filepath.Base(url), resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("download %s exceeds size limit", filepath.Base(url))
	}
	return b, nil
}

func checksumFor(data []byte, filename string) (string, error) {
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) != filename {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("invalid SHA256 for %s", filename)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("invalid SHA256 for %s: %w", filename, err)
		}
		return strings.ToLower(fields[0]), nil
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("SHA256 for %s not found", filename)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func rollbackUpgrade(cfg Config, backup string) {
	_ = os.Remove(installedBinaryPath)
	_ = os.Rename(backup, installedBinaryPath)
	_ = exec.Command("systemctl", "restart", cfg.ServiceName).Run()
}

// Upgrade installs the rolling edge binary only after verifying the release
// checksum. Replacement is atomic and the previous binary is restored if the
// new daemon cannot restart and answer DNS locally.
func Upgrade(ctx context.Context, cfg Config) (bool, string, error) {
	if os.Geteuid() != 0 {
		return false, "", fmt.Errorf("升级需要 root 权限")
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return false, "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if strings.Count(cfg.Repository, "/") != 1 || strings.ContainsAny(cfg.Repository, " \t\r\n") {
		return false, "", fmt.Errorf("invalid repository %q", cfg.Repository)
	}

	filename := "smartunlock-linux-" + runtime.GOARCH
	base := "https://github.com/" + cfg.Repository + "/releases/download/edge/"
	checksums, err := downloadUpgradeFile(ctx, base+"SHA256SUMS", cfg.GitHubToken, 1<<20)
	if err != nil {
		return false, "", err
	}
	expected, err := checksumFor(checksums, filename)
	if err != nil {
		return false, "", err
	}
	binary, err := downloadUpgradeFile(ctx, base+filename, cfg.GitHubToken, 64<<20)
	if err != nil {
		return false, "", err
	}
	got := sha256.Sum256(binary)
	digest := hex.EncodeToString(got[:])
	if digest != expected {
		return false, "", fmt.Errorf("SHA256 mismatch: expected %s, got %s", expected, digest)
	}
	if current, err := fileSHA256(installedBinaryPath); err == nil && current == digest {
		return false, digest, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(installedBinaryPath), ".smartunlock-upgrade-*")
	if err != nil {
		return false, "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(binary); err != nil {
		tmp.Close()
		return false, "", err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return false, "", err
	}
	if err = tmp.Chmod(0755); err != nil {
		tmp.Close()
		return false, "", err
	}
	if err = tmp.Close(); err != nil {
		return false, "", err
	}
	if out, err := exec.Command(tmpName, "version").CombinedOutput(); err != nil {
		return false, "", fmt.Errorf("downloaded binary validation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}

	backup := installedBinaryPath + ".upgrade-backup"
	_ = os.Remove(backup)
	if err = os.Rename(installedBinaryPath, backup); err != nil {
		return false, "", err
	}
	if err = os.Rename(tmpName, installedBinaryPath); err != nil {
		_ = os.Rename(backup, installedBinaryPath)
		return false, "", err
	}
	if err = exec.CommandContext(ctx, "systemctl", "restart", cfg.ServiceName).Run(); err != nil {
		rollbackUpgrade(cfg, backup)
		return false, "", fmt.Errorf("new service failed to restart; restored previous binary: %w", err)
	}
	if !waitMainDNSReady(20 * time.Second) {
		rollbackUpgrade(cfg, backup)
		return false, "", fmt.Errorf("new service DNS check failed; restored previous binary")
	}
	_ = os.Remove(backup)
	return true, digest, nil
}
