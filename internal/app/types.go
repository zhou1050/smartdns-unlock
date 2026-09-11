package app

import "time"

type Platform struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Probe    string `json:"probe,omitempty"`
}

var Platforms = []Platform{
	{"netflix", "Netflix", "streaming", "netflix"}, {"disney", "Disney+", "streaming", ""}, {"youtube", "YouTube", "streaming", "youtube"},
	{"primevideo", "Prime Video", "streaming", ""}, {"max", "Max / HBO Max", "streaming", ""}, {"hulu", "Hulu", "streaming", ""},
	{"appletv", "Apple TV+", "streaming", ""}, {"spotify", "Spotify", "streaming", "spotify"}, {"tiktok", "TikTok", "streaming", ""},
	{"dazn", "DAZN", "streaming", ""}, {"bbciplayer", "BBC iPlayer", "streaming", "bbc"}, {"paramount", "Paramount+", "streaming", ""},
	{"peacock", "Peacock", "streaming", ""}, {"crunchyroll", "Crunchyroll", "streaming", ""}, {"abema", "ABEMA", "streaming", "abema"},
	{"bahamut", "Bahamut Anime", "streaming", "bahamut"}, {"bilibili", "Bilibili", "streaming", "bilibili"}, {"iqiyi", "iQIYI", "streaming", ""},
	{"viu", "Viu", "streaming", ""}, {"tvb", "TVB", "streaming", ""},
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
