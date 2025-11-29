package types

import (
	"encoding/json"
	"time"
)

// UsageCache holds cached API usage data
type UsageCache struct {
	UsagePercent float64   `json:"usage_percent"`
	ResetTime    time.Time `json:"reset_time"`
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
	Model     *SessionModel `json:"model"`
	SessionID string        `json:"session_id"`
	Cwd       string        `json:"cwd"`
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
