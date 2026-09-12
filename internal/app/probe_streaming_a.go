package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const disneyBrowserAuth = "ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"

func explicitGeoBlock(text string) bool {
	low := strings.ToLower(text)
	for _, marker := range []string{
		"forbidden-location",
		"unsupported_country",
		"unsupported country",
		"not available in your country",
		"not available in your region",
		"unavailable in your country",
		"unavailable in your region",
		"geo_blocked",
		"geoblocked",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func recursiveJSONBool(body, key string) (bool, bool) {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return false, false
	}
	val, ok := findJSONKey(v, key)
	if !ok {
		return false, false
	}
	b, ok := val.(bool)
	return b, ok
}

func disneyProbeDecision(tokenCode int, tokenBody string, graphCode int, graphBody, final string) (string, string) {
	region := strings.ToUpper(findJSONString(graphBody, "countryCode"))
	if explicitGeoBlock(tokenBody + " " + graphBody + " " + final) {
		return "fail", region
	}
	if supported, ok := recursiveJSONBool(graphBody, "inSupportedLocation"); ok {
		if supported {
			return "pass", region
		}
		return "fail", region
	}
	if tokenCode == 403 || tokenCode == 429 || graphCode == 403 || graphCode == 429 {
		return "unknown", region
	}
	if tokenCode >= 200 && tokenCode < 300 && graphCode >= 200 && graphCode < 300 && region != "" {
		return "pass", region
	}
	return "unknown", region
}

func probeDisney(ctx context.Context) ProbeResult {
	headers := map[string]string{
		"Authorization": "Bearer " + disneyBrowserAuth,
		"Content-Type":  "application/json; charset=UTF-8",
	}
	devicePayload := `{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`
	deviceCode, _, _, deviceBody, err := probeRequest(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/devices", headers, devicePayload)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	assertion := findJSONString(deviceBody, "assertion")
	if assertion == "" {
		if explicitGeoBlock(deviceBody) {
			return res("fail", "", "Disney+ device assertion explicitly geo blocked")
		}
		return res("unknown", "", fmt.Sprintf("Disney+ device assertion unavailable (HTTP %d)", deviceCode))
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("latitude", "0")
	form.Set("longitude", "0")
	form.Set("platform", "browser")
	form.Set("subject_token", assertion)
	form.Set("subject_token_type", "urn:bamtech:params:oauth:token-type:device")
	tokenHeaders := map[string]string{
		"Authorization": "Bearer " + disneyBrowserAuth,
		"Content-Type":  "application/x-www-form-urlencoded",
	}
	tokenCode, _, _, tokenBody, tokenErr := probeRequest(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/token", tokenHeaders, form.Encode())
	if tokenErr != nil {
		return res("unknown", "", tokenErr.Error())
	}
	if explicitGeoBlock(tokenBody) {
		return res("fail", "", "Disney+ token exchange geo blocked")
	}
	refreshToken := findJSONString(tokenBody, "refresh_token")
	if refreshToken == "" {
		return res("unknown", "", fmt.Sprintf("Disney+ refresh token unavailable (HTTP %d)", tokenCode))
	}

	refreshJSON, _ := json.Marshal(refreshToken)
	graphPayload := `{"query":"mutation refreshToken($input: RefreshTokenInput!) { refreshToken(refreshToken: $input) { activeSession { sessionId } } }","variables":{"input":{"refreshToken":` + string(refreshJSON) + `}}}`
	graphHeaders := map[string]string{
		"Authorization": disneyBrowserAuth,
		"Content-Type":  "application/json",
	}
	graphCode, _, _, graphBody, graphErr := probeRequest(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/graph/v1/device/graphql", graphHeaders, graphPayload)
	if graphErr != nil {
		return res("unknown", "", graphErr.Error())
	}
	_, final, _, homeErr := httpGet(ctx, "https://www.disneyplus.com/", map[string]string{"Accept-Language": "en"})
	if homeErr != nil {
		final = ""
	}
	status, region := disneyProbeDecision(tokenCode, tokenBody, graphCode, graphBody, final)
	switch status {
	case "pass":
		return res("pass", region, "Disney+ token and location checks passed")
	case "fail":
		return res("fail", region, "Disney+ explicit unsupported location")
	default:
		return res("unknown", region, "Disney+ location result inconclusive")
	}
}

func probePrimeVideo(ctx context.Context) ProbeResult {
	code, _, body, err := httpGet(ctx, "https://www.primevideo.com", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	if explicitGeoBlock(body) {
		return res("fail", "", "Prime Video explicit geo block")
	}
	if code == 403 || code == 429 || code >= 500 {
		return res("unknown", "", fmt.Sprintf("Prime Video HTTP %d without explicit geo decision", code))
	}
	re := regexp.MustCompile(`(?i)"currentTerritory"\s*:\s*"([a-z]{2})"`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return res("unknown", "", "Prime Video territory not found")
	}
	region := strings.ToUpper(m[1])
	checkURL := "https://ab9f7h23rcdn.eu.api.amazonvideo.com/cdp/appleedge/getDataByTransform/v1/apple/detail/vod/v1.kt?itemId=amzn1.dv.gti.e6b39984-2bb6-f7d0-33e4-08ec574947f0&deviceId=6F97F9CCFA2243F1A3C44BD3C7F7908E&deviceTypeId=A3JTVZS31ZJ340&density=2x&firmware=10.6800.16104.3&format=json&enabledFeatures=denarius.location.gen4.daric.siglos.siglosPartnerBilling.contentDescriptors.contentDescriptorsV2.productPlacement.zeno.seriesSearch.tapsV2.dateTimeLocalization.multiSourcedEvents.mseEventLevelOffers.liveWatchModal.lbv.daapi.maturityRatingDecoration.seasonTrailer.cleanSlate.xbdModalV2.xbdModalVdp.playbackPinV2.exploreTab.reactions.progBadging.atfEpTimeVis.prereleaseCx.vppaConsent.episodicRelease.movieVam.movieVamCatalog&journeyIngressContext=8%7CEgRzdm9k&osLocale=zh_Hans_CN&timeZoneId=Asia%2FShanghai&uxLocale=zh_CN"
	apiCode, _, apiBody, apiErr := httpGet(ctx, checkURL, map[string]string{"User-Agent": "PrimeVideo/10.68 (iPad; iOS 18.3.2; Scale/2.00)"})
	if apiErr != nil {
		return res("unknown", region, apiErr.Error())
	}
	low := strings.ToLower(apiBody)
	if strings.Contains(apiBody, "您的设备使用了 VPN 或代理服务连接互联网请禁用并重试") || strings.Contains(low, "vpn or proxy") || explicitGeoBlock(apiBody) {
		return res("fail", region, "Prime Video playback endpoint denied location/proxy")
	}
	if apiCode == 403 || apiCode == 429 || apiCode >= 500 {
		return res("unknown", region, fmt.Sprintf("Prime Video playback HTTP %d", apiCode))
	}
	return res("pass", region, "Prime Video territory and playback endpoint available")
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
	if explicitGeoBlock(tokenBody) {
		return res("fail", "", "Max token endpoint explicitly geo blocked")
	}
	if code == 403 || code == 429 || code >= 500 {
		return res("unknown", "", fmt.Sprintf("Max token HTTP %d", code))
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
	if explicitGeoBlock(low) || strings.Contains(low, "hulu is not available") {
		return res("fail", "", "Hulu explicit geo block")
	}
	if code == 403 || code == 429 || code >= 500 {
		return res("unknown", "", fmt.Sprintf("Hulu HTTP %d without explicit geo decision", code))
	}
	if code >= 200 && code < 400 && (strings.Contains(low, `"country":"us"`) || strings.Contains(low, `"countrycode":"us"`)) {
		return res("pass", "US", "Hulu reports US location")
	}
	return res("unknown", "", "Hulu response has no explicit geo decision")
}

func probeTikTok(ctx context.Context) ProbeResult {
	code, final, body, err := httpGet(ctx, "https://www.tiktok.com/", nil)
	if err != nil {
		return res("unknown", "", err.Error())
	}
	_, _, _, regionBody, regionErr := probeRequest(ctx, http.MethodPost, "https://www.tiktok.com/passport/web/store_region/", nil, "")
	if regionErr == nil {
		region := findJSONString(regionBody, "store_region")
		if region == "" {
			region = findJSONString(regionBody, "region")
		}
		region = strings.ToUpper(region)
		if region != "" {
			return res("pass", region, "TikTok store region returned")
		}
	}
	low := strings.ToLower(final + " " + body + " " + regionBody)
	if explicitGeoBlock(low) || strings.Contains(low, "unavailable") {
		return res("fail", "", "TikTok explicit unavailable region")
	}
	if code == 403 || code == 429 {
		return res("unknown", "", fmt.Sprintf("TikTok HTTP %d challenge without geo decision", code))
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
