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
	SevenDayPercent float64   `json:"seven_day_percent"`
	SevenDayResetTime time.Time `json:"seven_day_reset_time"`
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
	Updated string                  `json:"updated"`
	Models  map[string]ModelPricing `json:"models"`
}

// ModelPricing contains input/output token prices per million
type ModelPricing struct {
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
	Size             int            `json:"context_window_size"`
	CurrentUsage     *ContextUsage  `json:"current_usage"`
	UsedPercentage   *float64       `json:"used_percentage"`
	RemainingPercent *float64       `json:"remaining_percentage"`
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
