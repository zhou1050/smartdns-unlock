package app

import "time"

type Platform struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Probe    string `json:"probe,omitempty"`
}

// nativeProbeCanSelect is deliberately stricter than a probe's display
// result. Some services expose a public home page worldwide, so a 2xx response
// can be useful diagnostics without proving that login/catalog/playback works.
// Such weak probes must never automatically put users on the native route.
func nativeProbeCanSelect(p Platform, r ProbeResult) bool {
	if r.Status != "pass" {
		return false
	}
	switch p.Probe {
	case "paramount", "peacock", "crunchyroll", "claude", "copilot":
		return false
	default:
		return p.Probe != ""
	}
}

var Platforms = []Platform{
	{"netflix", "Netflix", "streaming", "netflix"}, {"disney", "Disney+", "streaming", "disney"}, {"youtube", "YouTube", "streaming", "youtube"},
	{"primevideo", "Prime Video", "streaming", "primevideo"}, {"max", "Max / HBO Max", "streaming", "max"}, {"hulu", "Hulu", "streaming", "hulu"},
	{"appletv", "Apple TV+", "streaming", ""}, {"spotify", "Spotify", "streaming", "spotify"}, {"tiktok", "TikTok", "streaming", "tiktok"},
	{"dazn", "DAZN", "streaming", "dazn"}, {"bbciplayer", "BBC iPlayer", "streaming", "bbc"}, {"paramount", "Paramount+", "streaming", "paramount"},
	{"peacock", "Peacock", "streaming", "peacock"}, {"crunchyroll", "Crunchyroll", "streaming", "crunchyroll"}, {"abema", "ABEMA", "streaming", "abema"},
	{"bahamut", "Bahamut Anime", "streaming", "bahamut"}, {"bilibili", "Bilibili", "streaming", "bilibili"}, {"iqiyi", "iQIYI", "streaming", "iqiyi"},
	{"viu", "Viu", "streaming", "viu"}, {"tvb", "TVB", "streaming", "tvb"},
	{"openai", "ChatGPT / OpenAI", "ai", "openai"}, {"claude", "Claude", "ai", "claude"}, {"gemini", "Google Gemini", "ai", ""},
	{"githubcopilot", "GitHub Copilot", "ai", ""}, {"microsoftcopilot", "Microsoft Copilot", "ai", "copilot"}, {"perplexity", "Perplexity", "ai", ""},
	{"grok", "Grok / xAI", "ai", ""}, {"poe", "Poe", "ai", ""}, {"midjourney", "Midjourney", "ai", ""}, {"suno", "Suno", "ai", ""},
	{"deepseek", "DeepSeek", "ai", ""}, {"cursor", "Cursor", "ai", ""}, {"canva", "Canva", "ai", ""}, {"notion", "Notion AI", "ai", ""},
	{"characterai", "Character.AI", "ai", ""}, {"runway", "Runway", "ai", ""}, {"mistral", "Mistral AI", "ai", ""},
	{"huggingface", "Hugging Face", "ai", ""}, {"openrouter", "OpenRouter", "ai", ""},
}

type ProbeResult struct {
	Status string    `json:"status"`
	Region string    `json:"region,omitempty"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

type State struct {
	Version          int                    `json:"version"`
	Initialized      bool                   `json:"initialized"`
	Routes           map[string]string      `json:"routes"`
	RouteModes       map[string]string      `json:"route_modes,omitempty"`
	LastChecks       map[string]ProbeResult `json:"last_checks,omitempty"`
	PrimaryHealthy   bool                   `json:"primary_healthy"`
	BackupHealthy    bool                   `json:"backup_healthy"`
	RulesUpdatedAt   time.Time              `json:"rules_updated_at,omitempty"`
	LastPlatformScan time.Time              `json:"last_platform_scan,omitempty"`
}

type RulesFile struct {
	Version   int                 `json:"version"`
	UpdatedAt time.Time           `json:"updated_at"`
	Rules     map[string][]string `json:"rules"`
}
