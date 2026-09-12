package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func probeSpotifyStrict(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://www.spotify.com/tw/signup", map[string]string{"Accept-Language": "en"})
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code >= 500 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	low := strings.ToLower(body)
	if code == 403 || code == 451 || strings.Contains(low, "spotify is currently not available in your country") {
		return res("fail", "", "Spotify country blocked")
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)"geoCountry"\s*:\s*"([a-z]{2})"`),
		regexp.MustCompile(`(?i)geoCountry\\?"?\s*[:=]\s*\\?"([a-z]{2})`),
	}
	for _, re := range patterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return res("pass", strings.ToUpper(m[1]), "Spotify geoCountry returned")
		}
	}
	return res("unknown", "", "Spotify geoCountry unavailable")
}

func probeAbemaStrict(ctx context.Context) ProbeResult {
	code, _, _, body, err := probeRequest(ctx, http.MethodGet, "https://api.abema.io/v1/ip/check?device=android", map[string]string{
		"User-Agent": "Dalvik/2.1.0 (Linux; U; Android 9; ALP-AL00 Build/HUAWEIALP-AL00)",
	}, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code >= 500 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	region := strings.ToUpper(findJSONString(body, "isoCountryCode"))
	if region == "" {
		if code == 403 || code == 451 {
			return res("fail", "", fmt.Sprintf("HTTP %d", code))
		}
		return res("fail", "", "ABEMA country code unavailable")
	}
	if region == "JP" {
		return res("pass", region, "ABEMA Japan available")
	}
	return res("unknown", region, "ABEMA overseas-only response")
}

func probeBahamutStrict(ctx context.Context) ProbeResult {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	client := &http.Client{Timeout: 12 * time.Second, Jar: jar}
	get := func(rawURL string) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("User-Agent", ua)
		resp, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, string(b), err
	}
	code, body, err := get("https://ani.gamer.com.tw/ajax/getdeviceid.php")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("device HTTP %d", code))
	}
	device := findJSONString(body, "deviceid")
	if device == "" {
		return res("unknown", "", "Bahamut device id unavailable")
	}
	check := func(sn string) (bool, error) {
		u := "https://ani.gamer.com.tw/ajax/token.php?adID=89422&sn=" + url.QueryEscape(sn) + "&device=" + url.QueryEscape(device)
		status, b, err := get(u)
		if err != nil {
			return false, err
		}
		if status/100 != 2 {
			return false, fmt.Errorf("token HTTP %d", status)
		}
		var v map[string]any
		if json.Unmarshal([]byte(b), &v) != nil {
			return false, nil
		}
		_, ok := v["animeSn"]
		return ok, nil
	}
	tw, err := check("38832")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	hkmo, err := check("37783")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if tw && hkmo {
		return res("pass", "TW", "Bahamut playback token available")
	}
	if hkmo {
		return res("pass", "HK/MO", "Bahamut playback token available")
	}
	return res("fail", "", "Bahamut playback token unavailable")
}

func probeOpenAIStrict(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://ios.chat.openai.com", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(body)
	if code == 403 || code == 451 || strings.Contains(low, "blocked_why_headline") || strings.Contains(low, "unsupported_country_region_territory") || strings.Contains(low, "unsupported_country") {
		return res("fail", "", "OpenAI region blocked")
	}
	region := ""
	cf := ""
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) == nil {
		if details, ok := v["cf_details"].(map[string]any); ok {
			if c, ok := details["country"].(string); ok {
				region = strings.ToUpper(c)
			}
			b, _ := json.Marshal(details)
			cf = string(b)
		} else if raw, ok := v["cf_details"]; ok {
			b, _ := json.Marshal(raw)
			cf = string(b)
		}
	}
	if strings.Contains(cf, "(1)") || strings.Contains(cf, "(2)") {
		return res("unknown", region, "OpenAI web-only/disallowed ISP")
	}
	if region == "" {
		_, _, trace, traceErr := httpGet(ctx, "https://chatgpt.com/cdn-cgi/trace", nil)
		if traceErr == nil {
			for _, line := range strings.Split(trace, "\n") {
				if strings.HasPrefix(line, "loc=") {
					region = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "loc=")))
					break
				}
			}
		}
	}
	if (code/100 == 2 || code == 404) && region != "" {
		return res("pass", region, "OpenAI endpoint and region available")
	}
	return res("unknown", region, fmt.Sprintf("OpenAI HTTP %d without explicit full-access result", code))
}
