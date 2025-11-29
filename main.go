package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

//go:embed pricing.json
var embeddedPricing []byte

// Configuration
var (
	cacheTTL    int
	noColor     bool
	displayMode string
	infoMode    string
	debug       bool
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[38;5;248m"
	bgRed        = "\033[41m"
	bgGreen      = "\033[42m"
	bgYellow     = "\033[43m"
	bgBlue       = "\033[44m"
	bgMagenta    = "\033[45m"
	bgCyan       = "\033[46m"
)

// Structs
type UsageCache struct {
	UsagePercent float64   `json:"usage_percent"`
	ResetTime    time.Time `json:"reset_time"`
}

type UsageResponse struct {
	FiveHour *UsageWindow `json:"five_hour"`
	SevenDay *UsageWindow `json:"seven_day"`
}

type UsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

type Credentials struct {
	ClaudeAiOauth *OAuthCredentials `json:"claudeAiOauth"`
}

type OAuthCredentials struct {
	AccessToken      string      `json:"accessToken"`
	RefreshToken     string      `json:"refreshToken"`
	ExpiresAt        json.Number `json:"expiresAt"`
	SubscriptionType string      `json:"subscriptionType"`
	RateLimitTier    string      `json:"rateLimitTier"`
}

type PricingData struct {
	Updated string                     `json:"updated"`
	Models  map[string]ModelPricing    `json:"models"`
}

type ModelPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

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

type TokenStats struct {
	DailyCost   float64
	WeeklyCost  float64
	MonthlyCost float64
}

// SessionInput is the JSON input from Claude Code via stdin
type SessionInput struct {
	Model     string `json:"model"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
}

func main() {
	parseFlags()

	// Read session input from stdin (if available)
	session := readSessionInput()

	// Get all the status components
	gitInfo := getGitInfo()
	usage, subscription := getUsageAndSubscription()
	tokenStats := getTokenStats()

	// Format and output
	output := formatStatusLine(session, gitInfo, usage, tokenStats, subscription)
	fmt.Print(output)
}

func readSessionInput() *SessionInput {
	// Check if stdin has data (non-blocking)
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		// No piped input
		return nil
	}

	var session SessionInput
	if err := json.NewDecoder(os.Stdin).Decode(&session); err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "Failed to parse session input: %v\n", err)
		}
		return nil
	}
	return &session
}

func parseFlags() {
	flag.IntVar(&cacheTTL, "cache-ttl", getEnvInt("CLAUDE_STATUSLINE_CACHE_TTL", 300), "Cache TTL in seconds")
	flag.BoolVar(&noColor, "no-color", false, "Disable ANSI colors")
	flag.StringVar(&displayMode, "display-mode", getEnv("CLAUDE_STATUS_DISPLAY_MODE", "colors"), "Display mode: colors|minimal|background")
	flag.StringVar(&infoMode, "info-mode", getEnv("CLAUDE_STATUS_INFO_MODE", "none"), "Info mode: none|emoji|text")
	flag.BoolVar(&debug, "debug", false, "Enable debug output")
	flag.Parse()
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// Git functions
type GitInfo struct {
	Branch      string
	HasUntracked bool
	HasStaged    bool
	HasModified  bool
	Ahead       int
	Behind      int
	IsRepo      bool
}

func getGitInfo() GitInfo {
	info := GitInfo{}

	// Check if we're in a git repo
	if _, err := runGitCommand("rev-parse", "--git-dir"); err != nil {
		return info
	}
	info.IsRepo = true

	// Get branch name
	if branch, err := runGitCommand("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(branch)
	}

	// Get status
	if status, err := runGitCommand("status", "--porcelain"); err == nil {
		lines := strings.Split(status, "\n")
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			if strings.HasPrefix(line, "??") {
				info.HasUntracked = true
			}
			if line[0] != ' ' && line[0] != '?' {
				info.HasStaged = true
			}
			if line[1] != ' ' && line[1] != '?' {
				info.HasModified = true
			}
		}
	}

	// Get ahead/behind
	if counts, err := runGitCommand("rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		parts := strings.Fields(counts)
		if len(parts) == 2 {
			info.Behind, _ = strconv.Atoi(parts[0])
			info.Ahead, _ = strconv.Atoi(parts[1])
		}
	}

	return info
}

func runGitCommand(args ...string) (string, error) {
	cmdArgs := append([]string{"--no-optional-locks"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	return out.String(), err
}

// Usage functions
func getUsageAndSubscription() (*UsageCache, string) {
	cacheFile := getCacheFile("usage.json")
	subscription := ""

	// Get subscription from credentials
	creds := getCredentials()
	if creds != nil && creds.ClaudeAiOauth != nil {
		subscription = creds.ClaudeAiOauth.SubscriptionType
	}

	// Check cache
	if cache, valid := loadCache(cacheFile); valid {
		if debug {
			fmt.Fprintf(os.Stderr, "Using cached usage: %.1f%%\n", cache.UsagePercent)
		}
		return cache, subscription
	}

	// Fetch from API
	usage, err := fetchUsage(creds)
	if err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "API error: %v\n", err)
		}
		// Return cached data even if expired, or nil
		if cache, _ := loadCacheIgnoreExpiry(cacheFile); cache != nil {
			return cache, subscription
		}
		return nil, subscription
	}

	// Save cache
	saveCache(cacheFile, usage)
	if debug {
		fmt.Fprintf(os.Stderr, "Fetched usage: %.1f%%\n", usage.UsagePercent)
	}
	return usage, subscription
}

func getCredentials() *Credentials {
	username := os.Getenv("USER")
	if username == "" {
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
	}

	secret, err := keyring.Get("Claude Code-credentials", username)
	if err != nil || secret == "" {
		return nil
	}

	var creds Credentials
	if err := json.Unmarshal([]byte(secret), &creds); err != nil {
		return nil
	}
	return &creds
}

func getCacheFile(name string) string {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "claude-code-statusline")
	os.MkdirAll(cacheDir, 0755)
	return filepath.Join(cacheDir, name)
}

func loadCache(file string) (*UsageCache, bool) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, false
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}

	var cache UsageCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}

	// Determine TTL based on usage
	ttl := time.Duration(cacheTTL) * time.Second
	if cache.UsagePercent >= 95 {
		ttl = 0 // Always refresh
	} else if cache.UsagePercent >= 90 {
		ttl = 1 * time.Minute
	}

	// Check if cache is still valid
	if time.Since(info.ModTime()) > ttl {
		return &cache, false
	}

	return &cache, true
}

func loadCacheIgnoreExpiry(file string) (*UsageCache, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var cache UsageCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

func saveCache(file string, cache *UsageCache) {
	data, _ := json.Marshal(cache)
	os.WriteFile(file, data, 0644)
}

func fetchUsage(creds *Credentials) (*UsageCache, error) {
	if creds == nil || creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("no access token available")
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+creds.ClaudeAiOauth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var usageResp UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, err
	}

	// Use five_hour window as the primary usage metric
	if usageResp.FiveHour == nil {
		return nil, fmt.Errorf("no five_hour usage data")
	}

	resetTime, _ := time.Parse(time.RFC3339, usageResp.FiveHour.ResetsAt)
	return &UsageCache{
		UsagePercent: usageResp.FiveHour.Utilization,
		ResetTime:    resetTime,
	}, nil
}

// Token stats functions
func getTokenStats() *TokenStats {
	stats := &TokenStats{}
	pricing := loadPricing()
	seen := make(map[string]bool)

	now := time.Now()
	dailyCutoff := now.AddDate(0, 0, -1)
	weeklyCutoff := now.AddDate(0, 0, -7)
	monthlyCutoff := now.AddDate(0, -1, 0)

	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")

	if debug {
		fmt.Fprintf(os.Stderr, "Scanning logs from: %s\n", projectsDir)
	}

	filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		// Skip files older than monthly cutoff for performance
		if info.ModTime().Before(monthlyCutoff) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			var entry LogEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}

			// Parse timestamp
			ts, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil || ts.Before(monthlyCutoff) {
				continue
			}

			// Only process assistant messages with usage data
			if entry.Type != "assistant" {
				continue
			}

			// Deduplicate by message ID + request ID
			key := entry.Message.ID + ":" + entry.RequestID
			if key == ":" || seen[key] {
				continue
			}
			seen[key] = true

			// Get token counts from message.usage
			inputTokens := entry.Message.Usage.InputTokens
			outputTokens := entry.Message.Usage.OutputTokens
			cacheCreation := entry.Message.Usage.CacheCreationInputTokens
			cacheRead := entry.Message.Usage.CacheReadInputTokens

			if inputTokens == 0 && outputTokens == 0 && cacheCreation == 0 && cacheRead == 0 {
				continue
			}

			// Calculate cost based on model
			// Cache read tokens are discounted (10% of input price)
			// Cache creation tokens are charged at 1.25x input price
			var cost float64
			model := entry.Message.Model
			if p, ok := pricing.Models[model]; ok {
				cost += float64(inputTokens) / 1000000 * p.Input
				cost += float64(cacheCreation) / 1000000 * p.Input * 1.25
				cost += float64(cacheRead) / 1000000 * p.Input * 0.1
				cost += float64(outputTokens) / 1000000 * p.Output
			} else {
				// Default to sonnet pricing for unknown models
				cost += float64(inputTokens) / 1000000 * 3.0
				cost += float64(cacheCreation) / 1000000 * 3.0 * 1.25
				cost += float64(cacheRead) / 1000000 * 3.0 * 0.1
				cost += float64(outputTokens) / 1000000 * 15.0
			}

			// Add to appropriate buckets
			stats.MonthlyCost += cost
			if ts.After(weeklyCutoff) {
				stats.WeeklyCost += cost
			}
			if ts.After(dailyCutoff) {
				stats.DailyCost += cost
			}
		}

		return nil
	})

	if debug {
		fmt.Fprintf(os.Stderr, "Cost stats: daily=$%.2f, weekly=$%.2f, monthly=$%.2f\n",
			stats.DailyCost, stats.WeeklyCost, stats.MonthlyCost)
	}

	return stats
}

func loadPricing() *PricingData {
	var pricing PricingData

	// Try to load from cache first (for updated pricing)
	cacheFile := getCacheFile("pricing.json")
	if data, err := os.ReadFile(cacheFile); err == nil {
		if json.Unmarshal(data, &pricing) == nil {
			return &pricing
		}
	}

	// Fall back to embedded pricing
	json.Unmarshal(embeddedPricing, &pricing)
	return &pricing
}

// Formatting functions
func formatStatusLine(session *SessionInput, git GitInfo, usage *UsageCache, stats *TokenStats, subscription string) string {
	var parts []string

	// Directory
	cwd, _ := os.Getwd()
	dir := filepath.Base(cwd)
	if home := os.Getenv("HOME"); strings.HasPrefix(cwd, home) {
		dir = "~" + cwd[len(home):]
		if len(dir) > 20 {
			dir = "~/" + filepath.Base(cwd)
		}
	}
	parts = append(parts, colorize(dir, colorBlue, bgBlue))

	// Git info
	if git.IsRepo {
		gitPart := git.Branch
		indicators := ""
		if git.HasUntracked {
			indicators += "?"
		}
		if git.HasStaged {
			indicators += "+"
		}
		if git.HasModified {
			indicators += "!"
		}
		if indicators != "" {
			gitPart += " " + indicators
		}
		if git.Ahead > 0 {
			gitPart += fmt.Sprintf(" ↑%d", git.Ahead)
		}
		if git.Behind > 0 {
			gitPart += fmt.Sprintf(" ↓%d", git.Behind)
		}
		parts = append(parts, colorize(gitPart, colorMagenta, bgMagenta))
	}

	// Model info
	if session != nil && session.Model != "" {
		modelName := formatModelName(session.Model)
		parts = append(parts, colorize(modelName, colorCyan, bgCyan))
	}

	// Subscription type
	if subscription != "" {
		parts = append(parts, colorize(subscription, colorGray, bgBlue))
	}

	// Cost breakdown: monthly / weekly / daily
	if stats.DailyCost > 0 || stats.WeeklyCost > 0 || stats.MonthlyCost > 0 {
		costPart := fmt.Sprintf("$%.2f/m $%.2f/w $%.2f/d",
			stats.MonthlyCost, stats.WeeklyCost, stats.DailyCost)
		parts = append(parts, colorize(costPart, colorCyan, bgCyan))
	}

	// API Usage info (at the end)
	if usage != nil {
		usageColor := colorGreen
		usageBg := bgGreen
		if usage.UsagePercent >= 90 {
			usageColor = colorRed
			usageBg = bgRed
		} else if usage.UsagePercent >= 75 {
			usageColor = colorYellow
			usageBg = bgYellow
		}

		usagePart := fmt.Sprintf("%.0f%%", usage.UsagePercent)

		// Reset time
		if !usage.ResetTime.IsZero() {
			remaining := time.Until(usage.ResetTime)
			if remaining > 0 {
				usagePart += " " + formatDuration(remaining)
			}
		}

		parts = append(parts, colorize(usagePart, usageColor, usageBg))
	}

	// Add info mode prefixes
	if infoMode == "emoji" {
		for i, part := range parts {
			switch i {
			case 0:
				parts[i] = "📁 " + part
			case 1:
				if git.IsRepo {
					parts[i] = "🔀 " + part
				}
			}
		}
	} else if infoMode == "text" {
		for i, part := range parts {
			switch i {
			case 0:
				parts[i] = "Dir: " + part
			case 1:
				if git.IsRepo {
					parts[i] = "Git: " + part
				}
			}
		}
	}

	return strings.Join(parts, " | ")
}

func colorize(text, fgColor, bgColor string) string {
	if noColor {
		return text
	}

	switch displayMode {
	case "minimal":
		return colorGray + text + colorReset
	case "background":
		return bgColor + " " + text + " " + colorReset
	default: // colors
		return fgColor + text + colorReset
	}
}

func formatModelName(model string) string {
	// Convert model ID to friendly name
	// e.g., "claude-opus-4-5-20251101" -> "opus-4.5"
	model = strings.TrimPrefix(model, "claude-")

	// Remove date suffix
	if idx := strings.LastIndex(model, "-20"); idx > 0 {
		model = model[:idx]
	}

	// Format version numbers
	model = strings.ReplaceAll(model, "-", ".")

	return model
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0m"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

