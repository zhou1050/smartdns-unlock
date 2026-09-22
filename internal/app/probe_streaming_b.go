package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func paramountProbeDecision(code int, final, body string) string {
	low := strings.ToLower(final + " " + body)
	if explicitGeoBlock(low) || strings.Contains(low, "/intl/") || strings.HasSuffix(strings.TrimRight(strings.ToLower(final), "/"), "/intl") {
		return "fail"
	}
	if code == 403 || code == 429 || code >= 500 {
		return "unknown"
	}
	// The public landing page is globally reachable and does not prove catalog
	// or playback access.
	return "unknown"
}

func probeParamount(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.paramountplus.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	switch paramountProbeDecision(code, final, body) {
	case "fail":
		return res("fail", "", "Paramount+ explicit international/geo block")
	case "pass":
		region := "US"
		if u, err := url.Parse(final); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) > 0 && len(parts[0]) == 2 {
				region = strings.ToUpper(parts[0])
			}
		}
		return res("pass", region, "Paramount+ service page available")
	default:
		return res("unknown", "", fmt.Sprintf("Paramount+ HTTP %d without explicit geo decision", code))
	}
}

func peacockProbeDecision(code int, final, body string) string {
	low := strings.ToLower(final + " " + body)
	if explicitGeoBlock(low) || strings.Contains(low, "peacock is unavailable") || strings.Contains(low, "peacock is not available") {
		return "fail"
	}
	if code == 403 || code == 429 || code >= 500 {
		return "unknown"
	}
	// The public landing page is not a playback capability check.
	return "unknown"
}

func probePeacock(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.peacocktv.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	switch peacockProbeDecision(code, final, body) {
	case "fail":
		return res("fail", "", "Peacock explicit regional unavailability")
	case "pass":
		return res("pass", "US", "Peacock service page available")
	default:
		return res("unknown", "", fmt.Sprintf("Peacock HTTP %d without explicit geo decision", code))
	}
}

func crunchyrollProbeDecision(countryCode string, siteCode int, final, body string) string {
	low := strings.ToLower(final + " " + body)
	if explicitGeoBlock(low) || strings.Contains(low, "crunchyroll is not available") || strings.Contains(low, "service is not available in your region") {
		return "fail"
	}
	if siteCode == 403 || siteCode == 429 || siteCode >= 500 {
		return "unknown"
	}
	// Evidon's country result plus a public home page cannot establish that the
	// Crunchyroll catalog or streams are usable.
	return "unknown"
}

func probeCrunchyroll(ctx context.Context) ProbeResult {
	code, _, geoBody, err := httpGet(ctx, "https://c.evidon.com/geo/country.js", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	region := ""
	if code/100 == 2 {
		re := regexp.MustCompile(`(?i)['"]code['"]\s*:\s*['"]([a-z]{2})['"]`)
		if m := re.FindStringSubmatch(geoBody); len(m) > 1 {
			region = strings.ToUpper(m[1])
		}
	}
	siteCode, final, siteBody, siteErr := httpGet(ctx, "https://www.crunchyroll.com/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	if siteErr != nil {
		return res("unknown", region, siteErr.Error())
	}
	switch crunchyrollProbeDecision(region, siteCode, final, siteBody) {
	case "fail":
		return res("fail", region, "Crunchyroll explicit regional block")
	case "pass":
		return res("pass", region, "Crunchyroll service available in detected region")
	default:
		return res("unknown", region, fmt.Sprintf("Crunchyroll HTTP %d without explicit geo decision", siteCode))
	}
}

func probeIQIYI(ctx context.Context) ProbeResult {
	code, _, headers, _, err := probeRequest(ctx, http.MethodHead, "https://www.iq.com/", nil, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code >= 500 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	region := ""
	if v := headers.Get("X-Custom-Client-IP"); v != "" {
		parts := strings.Split(v, ":")
		region = strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	}
	mod := ""
	for _, vals := range headers {
		for _, v := range vals {
			for _, part := range strings.Split(v, ";") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(strings.ToLower(part), "mod=") {
					mod = strings.ToLower(strings.TrimSpace(part[len("mod="):]))
				}
			}
		}
	}
	if region == "CN" {
		return res("unknown", region, "iQIYI mainland mode")
	}
	if mod == "" {
		return res("unknown", region, "iQIYI mode unavailable")
	}
	if mod == "intl" {
		return res("fail", region, "iQIYI reports intl-blocked mode")
	}
	return res("pass", region, "iQIYI overseas mode available")
}

func viuRegion(final string) string {
	u, err := url.Parse(final)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && strings.EqualFold(parts[0], "ott") {
		return strings.ToUpper(parts[1])
	}
	if len(parts) > 0 {
		return strings.ToUpper(parts[0])
	}
	return ""
}

func viuProbeDecision(code int, final, body string) (string, string) {
	region := viuRegion(final)
	if region == "NO-SERVICE" {
		return "fail", ""
	}
	if explicitGeoBlock(final + " " + body) {
		return "fail", region
	}
	if code == 403 || code == 429 || code >= 500 {
		return "unknown", region
	}
	if region != "" && code >= 200 && code < 400 {
		return "pass", region
	}
	return "unknown", region
}

func probeViu(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.viu.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	status, region := viuProbeDecision(code, final, body)
	switch status {
	case "fail":
		return res("fail", region, "Viu explicit no-service/geo block")
	case "pass":
		return res("pass", region, "Viu service region available")
	default:
		return res("unknown", region, fmt.Sprintf("Viu HTTP %d without explicit geo decision", code))
	}
}

func probeTVB(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://uapisfm.tvbanywhere.com.sg/geoip/check/platform/android", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	region := strings.ToUpper(jsonStringAt(body, "country"))
	allowed, ok := jsonBoolAt(body, "allow_in_this_country")
	if region == "HK" {
		return res("unknown", region, "TVBAnywhere serviced by myTV SUPER in HK")
	}
	if !ok {
		return res("unknown", region, "TVBAnywhere allowed flag unavailable")
	}
	if allowed {
		return res("pass", region, "TVBAnywhere location allowed")
	}
	return res("fail", region, "TVBAnywhere location blocked")
}
