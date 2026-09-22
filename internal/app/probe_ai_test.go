package app

import "testing"

func TestOpenAIProbeDecisionDoesNotPassWhenIOSDisagrees(t *testing.T) {
	apiBody := `{"cookie_requirements":{"required":false}}`
	iosBody := `{"error":{"code":"unsupported_country_region_territory"}}`
	if got := openAIProbeDecision(200, apiBody, 403, iosBody); got != "unknown" {
		t.Fatalf("expected unknown when OpenAI endpoints disagree, got %q", got)
	}
}

func TestOpenAIProbeDecisionRequiresMultipleBlockSignals(t *testing.T) {
	blocked := `{"error":{"code":"unsupported_country_region_territory"}}`
	if got := openAIProbeDecision(403, blocked, 403, blocked); got != "fail" {
		t.Fatalf("expected fail for two explicit regional blocks, got %q", got)
	}
}

func TestOpenAIProbeDecisionSingleBlockIsUnknown(t *testing.T) {
	blocked := `{"error":{"code":"unsupported_country_region_territory"}}`
	if got := openAIProbeDecision(0, "", 403, blocked); got != "unknown" {
		t.Fatalf("expected unknown for one blocking signal without corroboration, got %q", got)
	}
}

func TestClaudeChallengeIsNotGeoBlock(t *testing.T) {
	if got := claudeProbeDecision(403, "https://claude.ai/", "Just a moment...", "US"); got != "unknown" {
		t.Fatalf("expected unknown for Claude anti-bot challenge, got %q", got)
	}
}

func TestClaudeExplicitRegionBlockFails(t *testing.T) {
	if got := claudeProbeDecision(403, "https://claude.ai/unsupported-country", "Claude is not available in your country", "HK"); got != "fail" {
		t.Fatalf("expected fail for explicit Claude regional block, got %q", got)
	}
}

func TestTraceRegion(t *testing.T) {
	if got := traceRegion("ip=203.0.113.1\nloc=SG\ncolo=SIN\n"); got != "SG" {
		t.Fatalf("trace region=%q", got)
	}
}
