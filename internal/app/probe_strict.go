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
	if strings.Contains(low, "spotify is currently not available in your country") || explicitGeoBlock(low) {
		return res("fail", "", "Spotify explicit country block")
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
	if code == 403 || code == 429 {
		return res("unknown", "", fmt.Sprintf("Spotify HTTP %d challenge without geo decision", code))
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
	region := strings.ToUpper(findJSONString(body, "isoCountryCode"))
	if region == "JP" {
		return res("pass", region, "ABEMA Japan available")
	}
	if region != "" {
		return res("fail", region, "ABEMA reports non-JP location")
	}
	if explicitGeoBlock(body) || code == 451 {
		return res("fail", "", "ABEMA explicit region denial")
	}
	if code == 403 || code == 429 || code >= 500 {
		return res("unknown", "", fmt.Sprintf("ABEMA HTTP %d without country code", code))
	}
	return res("unknown", "", "ABEMA country code unavailable")
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
	check := func(sn string) (bool, bool, error) {
		u := "https://ani.gamer.com.tw/ajax/token.php?adID=89422&sn=" + url.QueryEscape(sn) + "&device=" + url.QueryEscape(device)
		status, b, err := get(u)
		if err != nil {
			return false, false, err
		}
		if status/100 != 2 {
			return false, explicitGeoBlock(b), fmt.Errorf("token HTTP %d", status)
		}
		var v map[string]any
		if json.Unmarshal([]byte(b), &v) != nil {
			return false, explicitGeoBlock(b), nil
		}
		_, ok := v["animeSn"]
		return ok, explicitGeoBlock(b), nil
	}
	tw, twBlocked, err := check("38832")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	hkmo, hkmoBlocked, err := check("37783")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if tw && hkmo {
		return res("pass", "TW", "Bahamut playback token available")
	}
	if hkmo {
		return res("pass", "HK/MO", "Bahamut playback token available")
	}
	if twBlocked && hkmoBlocked {
		return res("fail", "", "Bahamut playback endpoints explicitly region blocked")
	}
	return res("unknown", "", "Bahamut playback token unavailable without explicit geo block")
}
