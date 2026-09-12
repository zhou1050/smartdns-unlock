package app

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

func probeDisney(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.disneyplus.com/", map[string]string{"Accept-Language": "en"})
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final + " " + body)
	if code == 403 || code == 451 || strings.Contains(low, "unavailable") || strings.Contains(low, "not available in your region") {
		return res("fail", "", "Disney+ unavailable")
	}
	region := ""
	localeRE := regexp.MustCompile(`(?i)/(?:[a-z]{2}-)([a-z]{2})(?:/|$)`)
	if m := localeRE.FindStringSubmatch(final); len(m) > 1 {
		region = strings.ToUpper(m[1])
	}
	if region == "" {
		countryRE := regexp.MustCompile(`(?i)countryCode\\?"?\s*[:=]\s*\\?"([a-z]{2})`)
		if m := countryRE.FindStringSubmatch(body); len(m) > 1 {
			region = strings.ToUpper(m[1])
		}
	}
	if code >= 200 && code < 400 && region != "" {
		return res("pass", region, "Disney+ region identified")
	}
	return res("unknown", region, "Disney+ did not expose an explicit supported region")
}

func probePrimeVideo(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://www.primevideo.com", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code == 403 || code == 451 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	re := regexp.MustCompile(`(?i)"currentTerritory"\s*:\s*"([a-z]{2})"`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return res("unknown", "", "Prime Video territory not found")
	}
	region := strings.ToUpper(m[1])
	checkURL := "https://ab9f7h23rcdn.eu.api.amazonvideo.com/cdp/appleedge/getDataByTransform/v1/apple/detail/vod/v1.kt?itemId=amzn1.dv.gti.e6b39984-2bb6-f7d0-33e4-08ec574947f0&deviceId=6F97F9CCFA2243F1A3C44BD3C7F7908E&deviceTypeId=A3JTVZS31ZJ340&density=2x&firmware=10.6800.16104.3&format=json&enabledFeatures=denarius.location.gen4.daric.siglos.siglosPartnerBilling.contentDescriptors.contentDescriptorsV2.productPlacement.zeno.seriesSearch.tapsV2.dateTimeLocalization.multiSourcedEvents.mseEventLevelOffers.liveWatchModal.lbv.daapi.maturityRatingDecoration.seasonTrailer.cleanSlate.xbdModalV2.xbdModalVdp.playbackPinV2.exploreTab.reactions.progBadging.atfEpTimeVis.prereleaseCx.vppaConsent.episodicRelease.movieVam.movieVamCatalog&journeyIngressContext=8%7CEgRzdm9k&osLocale=zh_Hans_CN&timeZoneId=Asia%2FShanghai&uxLocale=zh_CN"
	_, _, apiBody, apiErr := httpGet(ctx, checkURL, map[string]string{"User-Agent": "PrimeVideo/10.68 (iPad; iOS 18.3.2; Scale/2.00)"})
	if apiErr != nil {
		return res("unknown", region, apiErr.Error())
	}
	low := strings.ToLower(apiBody)
	if strings.Contains(apiBody, "您的设备使用了 VPN 或代理服务连接互联网请禁用并重试") || strings.Contains(low, "vpn or proxy") {
		return res("fail", region, "Prime Video VPN/proxy detected")
	}
	return res("pass", region, "Prime Video territory available")
}

func probeMax(ctx context.Context) ProbeResult {
	headers := map[string]string{
		"x-device-info":  "beam/5.0.0 (desktop/desktop; Windows/10; afbb5daa-c327-461d-9460-d8e4b3ee4a1f/da0cdd94-5a39-42ef-aa68-54cbc1b852c3)",
		"x-disco-client": "WEB:10.15.7:dotcom-hbomax:7.7.0",
		"x-disco-params": "realm=bolt,bid=beam,features=ar",
	}
	code, _, _, tokenBody, err := probeRequest(ctx, http.MethodGet, "https://default.any-any.prd.api.hbomax.com/token?realm=bolt&deviceId=afbb5daa-c327-461d-9460-d8e4b3ee4a1f", headers, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code == 403 || code == 451 {
		return res("fail", "", fmt.Sprintf("token HTTP %d", code))
	}
	token := jsonStringAt(tokenBody, "data", "attributes", "token")
	if token == "" {
		return res("unknown", "", "Max token unavailable")
	}
	bootstrapHeaders := map[string]string{
		"x-disco-client": headers["x-disco-client"],
		"x-disco-params": headers["x-disco-params"],
		"Cookie":         "st=" + token,
	}
	_, _, _, bootBody, err := probeRequest(ctx, http.MethodPost, "https://default.any-any.prd.api.hbomax.com/session-context/headwaiter/v1/bootstrap", bootstrapHeaders, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	domain := jsonStringAt(bootBody, "routing", "domain")
	tenant := jsonStringAt(bootBody, "routing", "tenant")
	env := jsonStringAt(bootBody, "routing", "env")
	homeMarket := jsonStringAt(bootBody, "routing", "homeMarket")
	if domain == "" || tenant == "" || env == "" || homeMarket == "" {
		return res("unknown", "", "Max routing bootstrap incomplete")
	}
	userURL := fmt.Sprintf("https://default.%s-%s.%s.%s/users/me", tenant, homeMarket, env, domain)
	_, _, _, userBody, err := probeRequest(ctx, http.MethodGet, userURL, bootstrapHeaders, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	region := strings.ToUpper(jsonStringAt(userBody, "data", "attributes", "currentLocationTerritory"))
	if region == "" {
		return res("unknown", "", "Max current location unavailable")
	}
	_, _, homeBody, err := httpGet(ctx, "https://www.hbomax.com/", nil)
	if err != nil {
		return res("unknown", region, err.Error())
	}
	regionRE := regexp.MustCompile(`(?i)"url"\s*:\s*"/([a-z]{2})/[a-z]{2}"`)
	available := map[string]bool{}
	for _, m := range regionRE.FindAllStringSubmatch(homeBody, -1) {
		if len(m) > 1 {
			available[strings.ToUpper(m[1])] = true
		}
	}
	if len(available) == 0 {
		return res("unknown", region, "Max supported-region list unavailable")
	}
	if available[region] {
		return res("pass", region, "Max location supported")
	}
	return res("fail", region, "Max location unsupported")
}

func probeHulu(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.hulu.com/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	if err != nil {
		return res("unknown", "", err.Error())
	}
	low := strings.ToLower(final + " " + body)
	if code == 403 || code == 451 || strings.Contains(low, "geo_blocked") || strings.Contains(low, "not available in your region") || strings.Contains(low, "hulu is not available") {
		return res("fail", "", "Hulu geo blocked")
	}
	// A plain marketing page is not sufficient proof that playback is available.
	// Only accept an explicit US/location marker; otherwise stay conservative.
	if code >= 200 && code < 400 && (strings.Contains(low, `"country":"us"`) || strings.Contains(low, `"countrycode":"us"`)) {
		return res("pass", "US", "Hulu reports US location")
	}
	return res("unknown", "", "Hulu response has no explicit geo decision")
}

func probeTikTok(ctx context.Context) ProbeResult {
	code, final, _, err := httpGet(ctx, "https://www.tiktok.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if code == 403 || code == 451 {
		return res("fail", "", fmt.Sprintf("HTTP %d", code))
	}
	_, _, _, body, err := probeRequest(ctx, http.MethodPost, "https://www.tiktok.com/passport/web/store_region/", nil, "")
	if err != nil {
		return res("unknown", "", err.Error())
	}
	region := findJSONString(body, "store_region")
	if region == "" {
		region = findJSONString(body, "region")
	}
	region = strings.ToUpper(region)
	if region != "" {
		return res("pass", region, "TikTok store region returned")
	}
	if strings.Contains(strings.ToLower(final), "unavailable") {
		return res("fail", "", "TikTok redirected to unavailable page")
	}
	return res("unknown", "", "TikTok store region unavailable")
}

func probeDAZN(ctx context.Context) ProbeResult {
	payload := `{"LandingPageKey":"generic","Languages":"zh-CN,zh,en","Platform":"web","PlatformAttributes":{},"Manufacturer":"","PromoCode":"","Version":"2"}`
	_, _, _, body, err := probeRequest(ctx, http.MethodPost, "https://startup.core.indazn.com/misl/v5/Startup", map[string]string{"Content-Type": "application/json"}, payload)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	allowed, ok := jsonBoolAt(body, "Region", "isAllowed")
	region := strings.ToUpper(jsonStringAt(body, "Region", "GeolocatedCountry"))
	if !ok {
		return res("unknown", region, "DAZN allowed flag unavailable")
	}
	if allowed {
		return res("pass", region, "DAZN location allowed")
	}
	return res("fail", region, "DAZN location blocked")
}
