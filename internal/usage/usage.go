package usage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
	"github.com/zalando/go-keyring"
)

// GetUsageAndSubscription retrieves usage data and subscription info
func GetUsageAndSubscription() (*types.UsageCache, string, string) {
	cacheFile := getCacheFile("usage.json")
	subscription := ""
	tier := ""

	// Get subscription from credentials
	creds := getCredentials()
	if creds != nil && creds.ClaudeAiOauth != nil {
		subscription = creds.ClaudeAiOauth.SubscriptionType
		tier = creds.ClaudeAiOauth.RateLimitTier
	}

	cfg := config.Get()

	// Check cache
	if cache, valid := loadCache(cacheFile, cfg.CacheTTL); valid {
		config.DebugLog("Using cached usage: %.1f%%", cache.UsagePercent)
		return cache, subscription, tier
	}

	// Fetch from API
	usage, err := fetchUsage(creds)
	if err != nil {
		config.DebugLog("API error: %v", err)
		// Return cached data even if expired, or nil
		if cache, _ := loadCacheIgnoreExpiry(cacheFile); cache != nil {
			return cache, subscription, tier
		}
		return nil, subscription, tier
	}

	// Save cache
	saveCache(cacheFile, usage)
	config.DebugLog("Fetched usage: %.1f%%", usage.UsagePercent)
	return usage, subscription, tier
}

func getCredentials() *types.Credentials {
	// First, try reading from credentials file (preferred)
	credFile := filepath.Join(os.Getenv("HOME"), ".claude", "credentials.json")
	if data, err := os.ReadFile(credFile); err == nil {
		var creds types.Credentials
		if err := json.Unmarshal(data, &creds); err == nil {
			config.DebugLog("Loaded credentials from file: %s", credFile)
			return &creds
		}
		config.DebugLog("Failed to parse credentials file: %v", err)
	}

	// Fall back to system keyring (macOS moves credentials there automatically)
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		username := os.Getenv("USER")
		if username == "" {
			if u, err := user.Current(); err == nil {
				username = u.Username
			}
		}

		secret, err := keyring.Get("Claude Code-credentials", username)
		if err == nil && secret != "" {
			var creds types.Credentials
			if err := json.Unmarshal([]byte(secret), &creds); err == nil {
				config.DebugLog("Loaded credentials from system keyring")
				return &creds
			}
			config.DebugLog("Failed to parse keyring credentials: %v", err)
		} else if err != nil {
			config.DebugLog("Keyring access failed: %v", err)
		}
	}

	config.DebugLog("No credentials found")
	return nil
}

func getCacheFile(name string) string {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "claude-code-statusline")
	os.MkdirAll(cacheDir, 0755)
	return filepath.Join(cacheDir, name)
}

func loadCache(file string, cacheTTL int) (*types.UsageCache, bool) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, false
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}

	var cache types.UsageCache
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

func loadCacheIgnoreExpiry(file string) (*types.UsageCache, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var cache types.UsageCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

func saveCache(file string, cache *types.UsageCache) {
	data, _ := json.Marshal(cache)
	os.WriteFile(file, data, 0644)
}

func fetchUsage(creds *types.Credentials) (*types.UsageCache, error) {
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

	var usageResp types.UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, err
	}

	// Use five_hour window as the primary usage metric
	if usageResp.FiveHour == nil {
		return nil, fmt.Errorf("no five_hour usage data")
	}

	resetTime, _ := time.Parse(time.RFC3339, usageResp.FiveHour.ResetsAt)
	return &types.UsageCache{
		UsagePercent: usageResp.FiveHour.Utilization,
		ResetTime:    resetTime,
	}, nil
}
