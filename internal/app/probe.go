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
		return probeBilibiliStrict(ctx)
	case "iqiyi":
		return probeIQIYI(ctx)
	case "viu":
		return probeViu(ctx)
	case "tvb":
		return probeTVB(ctx)
	case "openai":
		return probeOpenAI(ctx)
	case "claude":
		return probeClaude(ctx)
	case "copilot":
		return probeCopilot(ctx)
	default:
		return res("unknown", "", "unsupported probe")
	}
}

func netflixExplicitBlock(text string) bool {
	low := strings.ToLower(text)
	for _, marker := range []string{
		"not available in your country",
		"not available in your region",
		"unavailable in your country",
		"unavailable in your region",
		"this title is not available",
		"this title isn’t available",
		"this title isn't available",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func netflixProbeDecision(code int, final, body string) string {
	text := final + " " + body
	if netflixExplicitBlock(text) {
		return "fail"
	}
	if code == 403 || code == 429 || code == 451 || code >= 500 {
		return "unknown"
	}
	low := strings.ToLower(text)
	if code >= 200 && code < 400 && strings.Contains(strings.ToLower(final), "netflix.com") && (strings.Contains(final, "/title/") || strings.Contains(low, "netflix")) {
		return "pass"
	}
	return "unknown"
}

func probeNetflix(ctx context.Context) ProbeResult {
	check := func(title string) string {
		code, final, body, err := httpGet(ctx, "https://www.netflix.com/title/"+title, map[string]string{"Accept-Language": "en"})
		if err != nil {
			return "unknown"
		}
		return netflixProbeDecision(code, final, body)
	}

	// 81280792 is the licensed/non-Original title used to decide full catalog
	// access.  70143836 is only a fallback discriminator: reaching it after the
	// licensed title is blocked means Originals-only access, not a full pass.
	licensed := check("81280792")
	if licensed == "pass" {
		return res("pass", "", "Netflix licensed title reachable (full catalog)")
	}
	original := check("70143836")
	status, detail := netflixFullAccessDecision(licensed, original)
	return res(status, "", detail)
}

func netflixFullAccessDecision(licensed, original string) (string, string) {
	switch licensed {
	case "pass":
		return "pass", "Netflix licensed title reachable (full catalog)"
	case "fail":
		if original == "pass" {
			return "fail", "Netflix Originals only; licensed title blocked"
		}
		return "fail", "Netflix licensed title explicitly region blocked"
	default:
		if original == "pass" {
			return "unknown", "Netflix Originals reachable; licensed title inconclusive"
		}
		return "unknown", "Netflix licensed title inconclusive"
	}
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

func traceRegion(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "loc=") {
			return strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(line, "loc=")))
		}
	}
	return ""
}

func openAIExplicitBlock(body string) bool {
	low := strings.ToLower(body)
	for _, marker := range []string{
		"unsupported_country_region_territory",
		"unsupported_country",
		"country, region, or territory not supported",
		"blocked_why_headline",
		"not available in your country",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func openAIProbeDecision(apiCode int, apiBody string, iosCode int, iosBody string) string {
	apiBlocked := openAIExplicitBlock(apiBody)
	iosBlocked := openAIExplicitBlock(iosBody)
	if apiBlocked && iosBlocked {
		return "fail"
	}
	apiPositive := apiCode >= 200 && apiCode < 400 && strings.TrimSpace(apiBody) != "" && !apiBlocked
	iosPositive := (iosCode >= 200 && iosCode < 400 || iosCode == 404) && strings.TrimSpace(iosBody) != "" && !iosBlocked
	if apiBlocked || iosBlocked {
		return "unknown"
	}
	if apiPositive && iosPositive {
		return "pass"
	}
	return "unknown"
}

func probeOpenAI(ctx context.Context) ProbeResult {
	apiHeaders := map[string]string{
		"Accept":        "*/*",
		"Authorization": "Bearer null",
		"Content-Type":  "application/json",
		"Origin":        "https://platform.openai.com",
		"Referer":       "https://platform.openai.com/",
	}
	apiCode, _, apiBody, apiErr := httpGet(ctx, "https://api.openai.com/compliance/cookie_requirements", apiHeaders)
	iosCode, _, iosBody, iosErr := httpGet(ctx, "https://ios.chat.openai.com/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	_, _, traceBody, _ := httpGet(ctx, "https://chatgpt.com/cdn-cgi/trace", nil)
	region := traceRegion(traceBody)

	decision := openAIProbeDecision(apiCode, apiBody, iosCode, iosBody)
	switch decision {
	case "pass":
		detail := "OpenAI multi-endpoint capability available"
		if openAIExplicitBlock(iosBody) {
			detail = "OpenAI web/API available; iOS probe reports regional block"
		}
		return res("pass", region, detail)
	case "fail":
		return res("fail", region, "OpenAI region blocked by multiple endpoints")
	}
	if apiErr != nil && iosErr != nil {
		return res("unknown", region, "OpenAI probe endpoints unreachable")
	}
	if openAIExplicitBlock(apiBody) || openAIExplicitBlock(iosBody) {
		return res("unknown", region, "OpenAI endpoints disagree on regional availability")
	}
	return res("unknown", region, fmt.Sprintf("OpenAI inconclusive api=%d ios=%d", apiCode, iosCode))
}

func claudeExplicitBlock(text string) bool {
	low := strings.ToLower(text)
	for _, marker := range []string{
		"unsupported-country",
		"unsupported_country",
		"country is not supported",
		"not available in your country",
		"not available in your region",
		"claude is not available in your country",
		"claude is not available in your region",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func claudeProbeDecision(code int, final, body, region string) string {
	if claudeExplicitBlock(final + " " + body) {
		return "fail"
	}
	// A public landing page (or a Cloudflare challenge) does not prove that
	// sign-in and conversations are available from this region.
	if code >= 200 && code < 400 && region != "" && strings.Contains(strings.ToLower(final), "claude.ai") {
		return "unknown"
	}
	return "unknown"
}

func probeClaude(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://claude.ai/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	_, _, traceBody, _ := httpGet(ctx, "https://claude.ai/cdn-cgi/trace", nil)
	region := traceRegion(traceBody)
	if err != nil {
		return res("unknown", region, err.Error())
	}
	switch claudeProbeDecision(code, final, body, region) {
	case "fail":
		return res("fail", region, "Claude explicit region block")
	default:
		return res("unknown", region, fmt.Sprintf("Claude page reachable/HTTP %d without capability proof", code))
	}
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
		return res("unknown", "", "Copilot public page reachable without capability proof")
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
