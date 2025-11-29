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
	"path/filepath"
	"regexp"
	"sort"
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
	plan        string
)

// Plan limits
var planLimits = map[string]struct {
	tokens   int
	messages int
}{
	"pro":    {19000, 250},
	"max5":   {88000, 1000},
	"max20":  {220000, 2000},
	"custom": {44000, 250},
}

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
	UsagePercent float64 `json:"usage_percent"`
	ResetTime    string  `json:"reset_time"`
}

type Credentials struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    string `json:"expiresAt"`
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
	Model     string `json:"model"`
	Message   struct {
		Usage struct {
			InputTokens             int `json:"inputTokens"`
			OutputTokens            int `json:"outputTokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens    int `json:"cache_read_input_tokens"`
		} `json:"usage"`
		ID string `json:"id"`
	} `json:"message"`
	Usage struct {
		InputTokens             int `json:"inputTokens"`
		OutputTokens            int `json:"outputTokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens    int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	MessageID string `json:"message_id"`
	RequestID string `json:"requestId"`
}

type TokenStats struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64
}

func main() {
	parseFlags()

	// Get all the status components
	gitInfo := getGitInfo()
	usage := getUsage()
	tokenStats := getTokenStats()

	// Format and output
	output := formatStatusLine(gitInfo, usage, tokenStats)
	fmt.Print(output)
}

func parseFlags() {
	flag.IntVar(&cacheTTL, "cache-ttl", getEnvInt("CLAUDE_STATUSLINE_CACHE_TTL", 300), "Cache TTL in seconds")
	flag.BoolVar(&noColor, "no-color", false, "Disable ANSI colors")
	flag.StringVar(&displayMode, "display-mode", getEnv("CLAUDE_STATUS_DISPLAY_MODE", "colors"), "Display mode: colors|minimal|background")
	flag.StringVar(&infoMode, "info-mode", getEnv("CLAUDE_STATUS_INFO_MODE", "none"), "Info mode: none|emoji|text")
	flag.StringVar(&plan, "plan", getEnv("CLAUDE_STATUS_PLAN", "max5"), "Plan type: pro|max5|max20|custom")
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
func getUsage() *UsageCache {
	cacheFile := getCacheFile("usage.json")

	// Check cache
	if cache, valid := loadCache(cacheFile); valid {
		return cache
	}

	// Fetch from API
	usage, err := fetchUsage()
	if err != nil {
		// Return cached data even if expired, or nil
		if cache, _ := loadCacheIgnoreExpiry(cacheFile); cache != nil {
			return cache
		}
		return nil
	}

	// Save cache
	saveCache(cacheFile, usage)
	return usage
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

func fetchUsage() (*UsageCache, error) {
	token, err := getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
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

	resetTime, _ := time.Parse(time.RFC3339, usageResp.ResetTime)
	return &UsageCache{
		UsagePercent: usageResp.UsagePercent,
		ResetTime:    resetTime,
	}, nil
}

func getAccessToken() (string, error) {
	// Try keyring first
	secret, err := keyring.Get("Claude Code-credentials", "default")
	if err == nil && secret != "" {
		var creds Credentials
		if err := json.Unmarshal([]byte(secret), &creds); err == nil {
			return creds.AccessToken, nil
		}
	}

	// Fallback to credentials file
	credFile := filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")
	data, err := os.ReadFile(credFile)
	if err != nil {
		return "", fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("failed to parse credentials: %w", err)
	}

	return creds.AccessToken, nil
}

// Token stats functions
func getTokenStats() *TokenStats {
	stats := &TokenStats{}
	pricing := loadPricing()
	seen := make(map[string]bool)

	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	cutoff := time.Now().AddDate(0, 0, -1) // Last 24 hours for daily stats

	filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		// Skip files older than cutoff for performance
		if info.ModTime().Before(cutoff.AddDate(0, 0, -1)) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		// Increase buffer size for long lines
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			var entry LogEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}

			// Parse timestamp
			ts, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil || ts.Before(cutoff) {
				continue
			}

			// Deduplicate
			id := entry.MessageID
			if id == "" {
				id = entry.Message.ID
			}
			reqID := entry.RequestID
			key := id + ":" + reqID
			if seen[key] {
				continue
			}
			seen[key] = true

			// Get token counts
			inputTokens := entry.Message.Usage.InputTokens
			outputTokens := entry.Message.Usage.OutputTokens
			if inputTokens == 0 {
				inputTokens = entry.Usage.InputTokens
			}
			if outputTokens == 0 {
				outputTokens = entry.Usage.OutputTokens
			}

			if inputTokens == 0 && outputTokens == 0 {
				continue
			}

			stats.InputTokens += inputTokens
			stats.OutputTokens += outputTokens
			stats.TotalTokens += inputTokens + outputTokens

			// Calculate cost
			model := entry.Model
			if p, ok := pricing.Models[model]; ok {
				stats.Cost += float64(inputTokens) / 1000000 * p.Input
				stats.Cost += float64(outputTokens) / 1000000 * p.Output
			} else {
				// Default to sonnet pricing if model not found
				stats.Cost += float64(inputTokens) / 1000000 * 3.0
				stats.Cost += float64(outputTokens) / 1000000 * 15.0
			}
		}

		return nil
	})

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
func formatStatusLine(git GitInfo, usage *UsageCache, stats *TokenStats) string {
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

	// Usage info
	if usage != nil {
		limits := planLimits[plan]
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

		// Add token/message counts if we have stats
		if stats.TotalTokens > 0 {
			tokensStr := formatTokens(stats.TotalTokens)
			limitStr := formatTokens(limits.tokens)
			usagePart += fmt.Sprintf(" %s/%s", tokensStr, limitStr)
		}

		// Reset time
		if !usage.ResetTime.IsZero() {
			remaining := time.Until(usage.ResetTime)
			if remaining > 0 {
				usagePart += " " + formatDuration(remaining)
			}
		}

		parts = append(parts, colorize(usagePart, usageColor, usageBg))
	}

	// Cost
	if stats.Cost > 0 {
		costPart := fmt.Sprintf("$%.2f", stats.Cost)
		parts = append(parts, colorize(costPart, colorCyan, bgCyan))
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

func formatTokens(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000)
	}
	return strconv.Itoa(tokens)
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

// Unused but kept for potential future use
var _ = sort.Strings
var _ = regexp.MustCompile
