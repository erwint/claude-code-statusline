package types

import (
	"encoding/json"
	"time"
)

// UsageCache holds cached API usage data
type UsageCache struct {
	// 5-hour window
	UsagePercent float64   `json:"usage_percent"`
	ResetTime    time.Time `json:"reset_time"`

	// 7-day window
	SevenDayPercent   float64   `json:"seven_day_percent"`
	SevenDayResetTime time.Time `json:"seven_day_reset_time"`

	// Stale indicates the data may be outdated (e.g. in backoff after 429)
	Stale bool `json:"-"`
	// Unavailable indicates we can't reach the API and data has expired
	Unavailable bool `json:"-"`
}

// UsageResponse is the API response from Anthropic
type UsageResponse struct {
	FiveHour *UsageWindow `json:"five_hour"`
	SevenDay *UsageWindow `json:"seven_day"`
}

// UsageWindow represents a usage time window
type UsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// Credentials holds OAuth credentials from keychain
type Credentials struct {
	ClaudeAiOauth *OAuthCredentials `json:"claudeAiOauth"`
}

// OAuthCredentials contains the OAuth token data
type OAuthCredentials struct {
	AccessToken      string      `json:"accessToken"`
	RefreshToken     string      `json:"refreshToken"`
	ExpiresAt        json.Number `json:"expiresAt"`
	SubscriptionType string      `json:"subscriptionType"`
	RateLimitTier    string      `json:"rateLimitTier"`
}

// PricingData holds model pricing information
type PricingData struct {
	Updated string `json:"updated"`
	// Models is the price in effect today, keyed by model ID. Kept for
	// compatibility with statusline versions that predate History.
	Models map[string]ModelPricing `json:"models"`
	// History is the dated price list per model, oldest first. Prices change
	// over time (introductory rates, tier repricing), and logs are costed
	// against the rate that applied on the day they were written — so History
	// takes precedence over Models whenever it has an entry.
	History map[string][]PricePeriod `json:"history"`
}

// ModelPricing contains input/output token prices per million
type ModelPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// PricePeriod is a price that took effect on From and applies until the next
// period begins. An empty From means "since the beginning of time" — used when
// a model's earlier pricing history isn't known.
type PricePeriod struct {
	From   string  `json:"from,omitempty"` // YYYY-MM-DD
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// LogEntry represents a single log entry from Claude Code
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Message   struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			// CacheCreation splits the cache write total by TTL. The two tiers
			// are priced differently (5m at 1.25x input, 1h at 2x), and Claude
			// Code writes almost exclusively 1h entries.
			CacheCreation struct {
				Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
				Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
		ID string `json:"id"`
	} `json:"message"`
	RequestID string `json:"requestId"`
}

// TokenStats holds calculated cost statistics
type TokenStats struct {
	DailyCost   float64
	WeeklyCost  float64
	MonthlyCost float64
}

// SessionInput is the JSON input from Claude Code via stdin
type SessionInput struct {
	Model          *SessionModel  `json:"model"`
	SessionID      string         `json:"session_id"`
	Cwd            string         `json:"cwd"`
	TranscriptPath string         `json:"transcript_path"`
	ContextWindow  *ContextWindow `json:"context_window"`
}

// ContextWindow represents context usage from Claude Code
type ContextWindow struct {
	Size             int           `json:"context_window_size"`
	CurrentUsage     *ContextUsage `json:"current_usage"`
	UsedPercentage   *float64      `json:"used_percentage"`
	RemainingPercent *float64      `json:"remaining_percentage"`
}

// ContextUsage holds token counts for current usage
type ContextUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ToolEntry tracks a tool invocation from transcript
type ToolEntry struct {
	ID        string
	Name      string
	Target    string // e.g., file path for Read/Edit
	Status    string // "running" | "completed" | "error"
	StartTime time.Time
	EndTime   time.Time
}

// AgentEntry tracks a subagent (Task tool) from transcript
type AgentEntry struct {
	ID          string
	Type        string // "Explore", "Plan", etc.
	Description string
	Model       string
	Status      string // "running" | "completed"
	StartTime   time.Time
	EndTime     time.Time
}

// TodoItem tracks a todo from TodoWrite
type TodoItem struct {
	Subject string
	Status  string // "pending" | "in_progress" | "completed"
}

// TranscriptData holds parsed transcript information
type TranscriptData struct {
	Tools        []ToolEntry
	Agents       []AgentEntry
	Todos        []TodoItem
	SessionStart time.Time
}

// SessionModel contains model identification
type SessionModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// GitInfo holds git repository status
type GitInfo struct {
	Branch       string
	HasUntracked bool
	HasStaged    bool
	HasModified  bool
	Ahead        int
	Behind       int
	IsRepo       bool
}
