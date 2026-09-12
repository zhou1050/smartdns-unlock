package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func bilibiliAPICode(body string) (int, bool) {
	var v map[string]any
	if json.Unmarshal([]byte(body), &v) != nil {
		return 0, false
	}
	n, ok := v["code"].(float64)
	return int(n), ok
}

func bilibiliExplicitRegionBlock(body string) bool {
	low := strings.ToLower(body)
	for _, marker := range []string{
		"抱歉您所在地区不可观看",
		"所在地区不可观看",
		"地区限制",
		"地域限制",
		"area restricted",
		"region restricted",
	} {
		if strings.Contains(low, strings.ToLower(marker)) {
			return true
		}
	}
	var v any
	if json.Unmarshal([]byte(body), &v) == nil {
		if val, ok := findJSONKey(v, "area_limit"); ok {
			if b, ok := val.(bool); ok && b {
				return true
			}
		}
	}
	return false
}

func bilibiliProbeDecision(cnBody, hmtBody string) (string, string, string) {
	if code, ok := bilibiliAPICode(hmtBody); ok && code == 0 {
		return "pass", "HK/MO/TW", "Bilibili HK/MO/TW playback accepted"
	}
	if code, ok := bilibiliAPICode(cnBody); ok && code == 0 {
		return "pass", "CN", "Bilibili mainland playback accepted"
	}
	cnBlocked := bilibiliExplicitRegionBlock(cnBody)
	hmtBlocked := bilibiliExplicitRegionBlock(hmtBody)
	if cnBlocked && hmtBlocked {
		return "fail", "", "Bilibili regional playback explicitly blocked"
	}
	if cnBlocked || hmtBlocked {
		return "unknown", "", "Bilibili regional probes disagree"
	}
	cnCode, cnOK := bilibiliAPICode(cnBody)
	hmtCode, hmtOK := bilibiliAPICode(hmtBody)
	if cnOK || hmtOK {
		return "unknown", "", fmt.Sprintf("Bilibili API inconclusive cn=%d hmt=%d", cnCode, hmtCode)
	}
	return "unknown", "", "Bilibili API response format changed"
}

func probeBilibiliStrict(ctx context.Context) ProbeResult {
	cnURL := "https://api.bilibili.com/pgc/player/web/playurl?avid=82846771&qn=0&type=&otype=json&ep_id=307247&fourk=1&fnver=0&fnval=16&module=bangumi"
	hmtURL := "https://api.bilibili.com/pgc/player/web/playurl?avid=50762638&cid=100279344&qn=0&type=&otype=json&ep_id=268176&fourk=1&fnver=0&fnval=16&module=bangumi"
	cnCode, _, cnBody, cnErr := httpGet(ctx, cnURL, map[string]string{"Accept-Language": "zh-CN,zh;q=0.9"})
	hmtCode, _, hmtBody, hmtErr := httpGet(ctx, hmtURL, map[string]string{"Accept-Language": "zh-TW,zh;q=0.9"})
	if cnErr != nil && hmtErr != nil {
		return res("unknown", "", "Bilibili regional endpoints unreachable")
	}
	if cnErr != nil {
		cnBody = ""
	}
	if hmtErr != nil {
		hmtBody = ""
	}
	if cnCode >= 500 && hmtCode >= 500 {
		return res("unknown", "", fmt.Sprintf("Bilibili HTTP cn=%d hmt=%d", cnCode, hmtCode))
	}
	status, region, detail := bilibiliProbeDecision(cnBody, hmtBody)
	return res(status, region, detail)
}
