package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/126 Safari/537.36"

func httpGet(ctx context.Context, url string, headers map[string]string) (int, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	cl := &http.Client{Timeout: 12 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, resp.Request.URL.String(), "", err
	}
	return resp.StatusCode, resp.Request.URL.String(), string(b), nil
}

func res(status, region, detail string) ProbeResult {
	return ProbeResult{Status: status, Region: region, Detail: detail, At: time.Now()}
}

func ProbePlatform(ctx context.Context, p Platform) ProbeResult {
	if p.Probe == "" {
		return res("unknown", "", "no reliable native probe")
	}
	switch p.Probe {
	case "netflix":
		return probeNetflix(ctx)
	case "disney":
		return probeDisney(ctx)
	case "youtube":
		return probeYouTube(ctx)
	case "primevideo":
		return probePrimeVideo(ctx)
	case "max":
		return probeMax(ctx)
	case "hulu":
		return probeHulu(ctx)
	case "spotify":
		return probeSpotifyStrict(ctx)
	case "tiktok":
		return probeTikTok(ctx)
	case "dazn":
		return probeDAZN(ctx)
	case "bbc":
		return probeBBC(ctx)
	case "paramount":
		return probeParamount(ctx)
	case "peacock":
		return probePeacock(ctx)
	case "crunchyroll":
		return probeCrunchyroll(ctx)
	case "abema":
		return probeAbemaStrict(ctx)
	case "bahamut":
		return probeBahamutStrict(ctx)
	case "bilibili":
		return probeBilibili(ctx)
	case "iqiyi":
		return probeIQIYI(ctx)
	case "viu":
		return probeViu(ctx)
	case "tvb":
		return probeTVB(ctx)
	case "openai":
		return probeOpenAIStrict(ctx)
	case "claude":
		return probeClaude(ctx)
	case "copilot":
		return probeCopilot(ctx)
	default:
		return res("unknown", "", "unsupported probe")
	}
}

func probeNetflix(ctx context.Context) ProbeResult {
	for _, u := range []string{"https://www.netflix.com/title/81280792", "https://www.netflix.com/title/70143836"} {
		code, final, body, err := httpGet(ctx, u, map[string]string{"Accept-Language": "en"})
		if err != nil {
			return res("unknown", "", err.Error())
		}
		low := strings.ToLower(body + " " + final)
		if code == 403 || code == 451 || strings.Contains(low, "not available in your country") || strings.Contains(low, "unavailable in your region") {
			return res("fail", "", "region blocked")
		}
		if code/100 == 2 && (strings.Contains(final, "/title/") || strings.Contains(low, "netflix")) {
			return res("pass", "", "full title reachable")
		}
	}
	return res("fail", "", "Netflix title unavailable")
}

func probeYouTube(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://www.youtube.com/premium", map[string]string{"Accept-Language": "en"})
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "youtube premium is not available in your country") || strings.Contains(low, "not available in your country") {
		return res("fail", "", "Premium unavailable")
	}
	re := regexp.MustCompile(`(?i)countryCode\\?"?\s*[:=]\s*\\?"([A-Z]{2})`)
	m := re.FindStringSubmatch(body)
	region := ""
	if len(m) > 1 {
		region = m[1]
	}
	if strings.Contains(low, "youtube premium") {
		return res("pass", region, "Premium page available")
	}
	return res("unknown", region, "no explicit Premium result")
}

func probeSpotify(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://www.spotify.com/tw/signup", map[string]string{"Accept-Language": "en"})
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "spotify is currently not available in your country") {
		return res("fail", "", "country blocked")
	}
	return res("pass", "", "signup reachable")
}

func probeBBC(ctx context.Context) ProbeResult {
	u := "https://open.live.bbc.co.uk/mediaselector/6/select/version/2.0/mediaset/pc/vpid/bbc_one_london/format/json"
	code, _, body, err := httpGet(ctx, u, nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(body)
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	if strings.Contains(low, "geolocation") || strings.Contains(low, "notuk") || strings.Contains(low, "outside the uk") {
		return res("fail", "", "BBC geolocation denied")
	}
	if strings.Contains(low, "media") || strings.Contains(low, "connection") {
		return res("pass", "UK", "BBC media selector available")
	}
	return res("unknown", "", "unexpected BBC response")
}

func probeAbema(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://api.abema.io/v1/ip/check?device=android", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "not available") || strings.Contains(low, "false") || strings.Contains(low, "ng") {
		return res("fail", "", "ABEMA region denied")
	}
	return res("pass", "JP", "ABEMA IP check accepted")
}

func probeBahamut(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://ani.gamer.com.tw/ajax/getdeviceid.php", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	if strings.Contains(strings.ToLower(body), "device") || len(strings.TrimSpace(body)) > 10 {
		return res("pass", "TW/HK/MO", "device token available")
	}
	return res("unknown", "", "unexpected Bahamut response")
}

func probeBilibili(ctx context.Context) ProbeResult {
	u := "https://api.bilibili.com/pgc/player/web/playurl?avid=18281381&cid=29892777&qn=0&type=&otype=json&ep_id=183799&fourk=1&fnver=0&fnval=16&module=bangumi"
	code, _, body, err := httpGet(ctx, u, nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) == nil {
		if n, ok := v["code"].(float64); ok {
			if n == 0 {
				return res("pass", "HK/MO/TW", "Bilibili playurl accepted")
			}
			return res("fail", "", fmt.Sprintf("API code %.0f", n))
		}
	}
	return res("unknown", "", "unexpected Bilibili response")
}

func probeOpenAI(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://ios.chat.openai.com", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code == 403 || code == 451 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "unsupported_country") || strings.Contains(low, "disallowed") || strings.Contains(low, "not available in your country") {
		return res("fail", "", "OpenAI region/ISP denied")
	}
	region := ""
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) == nil {
		if cf, ok := v["cf_details"].(map[string]any); ok {
			if c, ok := cf["country"].(string); ok {
				region = c
			}
		}
	}
	if code/100 == 2 || code == 404 {
		return res("pass", region, "OpenAI endpoint reachable")
	}
	return res("unknown", region, fmt.Sprintf("HTTP %d", code))
}

func probeClaude(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://claude.ai/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final + " " + body)
	if strings.Contains(low, "unavailable") || code == 403 || code == 451 {
		return res("fail", "", "Claude unavailable")
	}
	if code >= 200 && code < 500 {
		return res("pass", "", "Claude reachable")
	}
	return res("unknown", "", fmt.Sprintf("HTTP %d", code))
}

func probeCopilot(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://copilot.microsoft.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final + " " + body)
	if code == 403 || code == 451 || strings.Contains(low, "not available in your region") {
		return res("fail", "", "Copilot region denied")
	}
	if code/100 == 2 {
		return res("pass", "", "Copilot reachable")
	}
	return res("unknown", "", fmt.Sprintf("HTTP %d", code))
}

func ProbeAll(ctx context.Context) map[string]ProbeResult {
	out := make(map[string]ProbeResult, len(Platforms))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, p := range Platforms {
		if p.Probe == "" {
			mu.Lock()
			out[p.ID] = res("unknown", "", "no reliable native probe")
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(p Platform) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				out[p.ID] = res("unknown", "", ctx.Err().Error())
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			c, cancel := context.WithTimeout(ctx, 15*time.Second)
			r := ProbePlatform(c, p)
			cancel()
			mu.Lock()
			out[p.ID] = r
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return out
}
