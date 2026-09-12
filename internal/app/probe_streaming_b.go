package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func probeParamount(ctx context.Context) ProbeResult {
	code, final, _, err := httpGet(ctx, "https://www.paramountplus.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final)
	if code == 403 || code == 451 || strings.Contains(low, "intl") {
		return res("fail", "", "Paramount+ geo blocked")
	}
	if code >= 200 && code < 400 {
		return res("pass", "", "Paramount+ main site available")
	}
	return res("unknown", "", fmt.Sprintf("HTTP %d", code))
}

func probePeacock(ctx context.Context) ProbeResult {
	code, final, _, err := httpGet(ctx, "https://www.peacocktv.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final)
	if code == 403 || code == 451 || strings.Contains(low, "unavailable") {
		return res("fail", "", "Peacock unavailable")
	}
	if code >= 200 && code < 400 {
		return res("pass", "US", "Peacock main site available")
	}
	return res("unknown", "", fmt.Sprintf("HTTP %d", code))
}

func probeCrunchyroll(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://c.evidon.com/geo/country.js", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code/100 != 2 {
		return res("unknown", "", fmt.Sprintf("HTTP %d", code))
	}
	re := regexp.MustCompile(`(?i)["']?country_code["']?\s*[:=]\s*["']([a-z]{2})["']`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return res("unknown", "", "Crunchyroll country code unavailable")
	}
	return res("pass", strings.ToUpper(m[1]), "Crunchyroll geo country returned")
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

func probeViu(ctx context.Context) ProbeResult {
	code, final, _, err := httpGet(ctx, "https://www.viu.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code == 403 || code == 451 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	_, _, banBody, banErr := httpGet(ctx, "https://d3o7oi00quuwqu.cloudfront.net", nil)
	if banErr != nil {
		return res("unknown", "", banErr.Error())
	}
	u, err := url.Parse(final)
	if err != nil {
		return res("unknown", "", "invalid Viu redirect")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	region := ""
	if len(parts) >= 2 && strings.EqualFold(parts[0], "ott") {
		region = strings.ToUpper(parts[1])
	} else if len(parts) > 0 {
		region = strings.ToUpper(parts[0])
	}
	if region == "" {
		return res("unknown", "", "Viu region unavailable")
	}
	if strings.Contains(strings.ToLower(banBody), "block access") {
		return res("fail", region, "Viu CDN blocks access")
	}
	return res("pass", region, "Viu region available")
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
