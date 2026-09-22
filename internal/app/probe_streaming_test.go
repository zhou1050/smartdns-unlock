package app

import "testing"

func TestNetflixChallengeDoesNotFail(t *testing.T) {
	if got := netflixProbeDecision(403, "https://www.netflix.com/title/81280792", "Access denied"); got != "unknown" {
		t.Fatalf("expected Netflix challenge unknown, got %q", got)
	}
}

func TestNetflixExplicitRegionBlockFails(t *testing.T) {
	if got := netflixProbeDecision(200, "https://www.netflix.com/title/81280792", "This title is not available in your region"); got != "fail" {
		t.Fatalf("expected Netflix explicit geo block fail, got %q", got)
	}
}

func TestNetflixReachableTitlePasses(t *testing.T) {
	if got := netflixProbeDecision(200, "https://www.netflix.com/title/81280792", "Netflix"); got != "pass" {
		t.Fatalf("expected Netflix title pass, got %q", got)
	}
}

func TestNetflixOriginalsOnlyDoesNotPass(t *testing.T) {
	status, detail := netflixFullAccessDecision("fail", "pass")
	if status != "fail" || detail == "" {
		t.Fatalf("expected Originals-only result to fail full unlock, got %q %q", status, detail)
	}
}

func TestNetflixLicensedInconclusiveDoesNotPass(t *testing.T) {
	status, _ := netflixFullAccessDecision("unknown", "pass")
	if status != "unknown" {
		t.Fatalf("expected licensed-title uncertainty to remain unknown, got %q", status)
	}
}

func TestDisneySupportedLocationPasses(t *testing.T) {
	graph := `{"extensions":{"sdk":{"session":{"location":{"countryCode":"SG","inSupportedLocation":true}}}}}`
	status, region := disneyProbeDecision(200, `{"refresh_token":"ok"}`, 200, graph, "https://www.disneyplus.com/")
	if status != "pass" || region != "SG" {
		t.Fatalf("Disney decision=%q region=%q", status, region)
	}
}

func TestDisneyChallengeDoesNotFail(t *testing.T) {
	status, _ := disneyProbeDecision(403, "Just a moment...", 403, "", "https://www.disneyplus.com/")
	if status != "unknown" {
		t.Fatalf("expected Disney challenge to be unknown, got %q", status)
	}
}

func TestDisneyExplicitGeoBlockFails(t *testing.T) {
	status, _ := disneyProbeDecision(403, `{"error":"forbidden-location"}`, 0, "", "")
	if status != "fail" {
		t.Fatalf("expected Disney forbidden-location fail, got %q", status)
	}
}

func TestParamountChallengeDoesNotFail(t *testing.T) {
	if got := paramountProbeDecision(403, "https://www.paramountplus.com/", "Access denied"); got != "unknown" {
		t.Fatalf("expected Paramount challenge unknown, got %q", got)
	}
}

func TestPeacockChallengeDoesNotFail(t *testing.T) {
	if got := peacockProbeDecision(403, "https://www.peacocktv.com/", "Just a moment..."); got != "unknown" {
		t.Fatalf("expected Peacock challenge unknown, got %q", got)
	}
}

func TestCrunchyrollPublicPageIsInconclusive(t *testing.T) {
	if got := crunchyrollProbeDecision("SG", 200, "https://www.crunchyroll.com/", "Crunchyroll"); got != "unknown" {
		t.Fatalf("expected public Crunchyroll page to remain unknown, got %q", got)
	}
}

func TestParamountPublicPageIsInconclusive(t *testing.T) {
	if got := paramountProbeDecision(200, "https://www.paramountplus.com/", "Paramount+"); got != "unknown" {
		t.Fatalf("expected public Paramount page to remain unknown, got %q", got)
	}
}

func TestPeacockPublicPageIsInconclusive(t *testing.T) {
	if got := peacockProbeDecision(200, "https://www.peacocktv.com/", "Peacock"); got != "unknown" {
		t.Fatalf("expected public Peacock page to remain unknown, got %q", got)
	}
}

func TestOpenAIEndpointsMustAgreeBeforePass(t *testing.T) {
	if got := openAIProbeDecision(200, `{}`, 403, "blocked_why_headline"); got != "unknown" {
		t.Fatalf("expected disagreeing OpenAI endpoints to remain unknown, got %q", got)
	}
	if got := openAIProbeDecision(200, `{}`, 200, `{}`); got != "pass" {
		t.Fatalf("expected two positive OpenAI endpoints to pass, got %q", got)
	}
}

func TestClaudeChallengeAndLandingPageAreInconclusive(t *testing.T) {
	for _, code := range []int{200, 403, 429} {
		if got := claudeProbeDecision(code, "https://claude.ai/", "Claude", "US"); got != "unknown" {
			t.Fatalf("Claude HTTP %d unexpectedly passed: %q", code, got)
		}
	}
}

func TestCrunchyrollExplicitRegionBlockFails(t *testing.T) {
	if got := crunchyrollProbeDecision("RU", 200, "https://www.crunchyroll.com/", "Service is not available in your region"); got != "fail" {
		t.Fatalf("expected explicit Crunchyroll geo block fail, got %q", got)
	}
}

func TestViuRegionRedirectPasses(t *testing.T) {
	status, region := viuProbeDecision(200, "https://www.viu.com/ott/hk/", "Viu")
	if status != "pass" || region != "HK" {
		t.Fatalf("Viu decision=%q region=%q", status, region)
	}
}

func TestViuNoServiceFails(t *testing.T) {
	status, _ := viuProbeDecision(200, "https://www.viu.com/no-service/", "")
	if status != "fail" {
		t.Fatalf("expected Viu no-service fail, got %q", status)
	}
}

func TestBilibiliStaleEpisodeErrorIsUnknown(t *testing.T) {
	status, _, _ := bilibiliProbeDecision(`{"code":-404,"message":"啥都木有"}`, `{"code":-404,"message":"啥都木有"}`)
	if status != "unknown" {
		t.Fatalf("expected stale Bilibili content error unknown, got %q", status)
	}
}

func TestBilibiliRegionalPlaybackPasses(t *testing.T) {
	status, region, _ := bilibiliProbeDecision(`{"code":-10403}`, `{"code":0,"result":{"durl":[]}}`)
	if status != "pass" || region != "HK/MO/TW" {
		t.Fatalf("Bilibili decision=%q region=%q", status, region)
	}
}
