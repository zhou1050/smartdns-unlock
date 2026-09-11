package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func SyncRules(ctx context.Context, cfg Config) (RulesFile, error) {
	if !strings.Contains(cfg.Repository, "/") {
		return RulesFile{}, fmt.Errorf("invalid repository")
	}
	var url string
	if cfg.GitHubToken != "" {
		url = "https://api.github.com/repos/" + cfg.Repository + "/tarball/main"
	} else {
		url = "https://github.com/" + cfg.Repository + "/archive/refs/heads/main.tar.gz"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RulesFile{}, err
	}
	if cfg.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	cl := &http.Client{Timeout: 90 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return RulesFile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return RulesFile{}, fmt.Errorf("rules download HTTP %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return RulesFile{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := RulesFile{Version: 1, UpdatedAt: time.Now(), Rules: map[string][]string{}}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return RulesFile{}, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		clean := filepath.ToSlash(h.Name)
		idx := strings.Index(clean, "/rules/generated/")
		if idx < 0 || !strings.HasSuffix(clean, ".txt") {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(clean), ".txt")
		if _, ok := PlatformByID(id); !ok {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, 2<<20))
		if err != nil {
			return RulesFile{}, err
		}
		var lines []string
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			lines = append(lines, ln)
		}
		if len(lines) > 0 {
			out.Rules[id] = lines
		}
	}
	if len(out.Rules) == 0 {
		return RulesFile{}, fmt.Errorf("no generated rules found")
	}
	if err := SaveRules(cfg.RulesPath, out); err != nil {
		return RulesFile{}, err
	}
	return out, nil
}
