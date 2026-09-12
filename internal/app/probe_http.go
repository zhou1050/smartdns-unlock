package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func closeProbeIdleConnections() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

func probeRequest(ctx context.Context, method, rawURL string, headers map[string]string, body string) (int, string, http.Header, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return 0, "", nil, "", err
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, "", nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, resp.Request.URL.String(), resp.Header.Clone(), "", err
	}
	return resp.StatusCode, resp.Request.URL.String(), resp.Header.Clone(), string(b), nil
}

func jsonValueAt(body string, path ...string) (any, bool) {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return nil, false
	}
	cur := v
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func jsonStringAt(body string, path ...string) string {
	if v, ok := jsonValueAt(body, path...); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func jsonBoolAt(body string, path ...string) (bool, bool) {
	if v, ok := jsonValueAt(body, path...); ok {
		b, ok := v.(bool)
		return b, ok
	}
	return false, false
}

func findJSONKey(v any, key string) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		if val, ok := x[key]; ok {
			return val, true
		}
		for _, val := range x {
			if found, ok := findJSONKey(val, key); ok {
				return found, true
			}
		}
	case []any:
		for _, val := range x {
			if found, ok := findJSONKey(val, key); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func findJSONString(body, key string) string {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return ""
	}
	if val, ok := findJSONKey(v, key); ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
