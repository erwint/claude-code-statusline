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
	"strconv"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
	"github.com/zalando/go-keyring"
)

// GetUsageAndSubscription retrieves usage data and subscription info
// Returns: usage data, subscription type, tier, and whether on API billing
func GetUsageAndSubscription() (*types.UsageCache, string, string, bool) {
	cacheFile := getCacheFile("usage.json")
	subscription := ""
	tier := ""
	isApiBilling := false

	// Detect API billing: check if ANTHROPIC_API_KEY is set (primary indicator)
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		isApiBilling = true
	}

	// Get subscription from credentials
	creds := getCredentials()
	if creds != nil && creds.ClaudeAiOauth != nil {
		subscription = creds.ClaudeAiOauth.SubscriptionType
		tier = creds.ClaudeAiOauth.RateLimitTier
	}

	cfg := config.Get()

	// Check cache
	if cache, valid := loadCache(cacheFile, cfg.CacheTTL); valid {
		// If the reset time has passed, force a refresh instead of using stale data
		if !cache.ResetTime.IsZero() && time.Now().After(cache.ResetTime) {
			config.DebugLog("Cache reset time has passed, forcing refresh")
		} else {
			config.DebugLog("Using cached usage: %.1f%%", cache.UsagePercent)
			return cache, subscription, tier, isApiBilling
		}
	}

	// Check backoff before hitting the API
	if b := loadBackoff(); b != nil && time.Now().Before(b.BackoffUntil) {
		config.DebugLog("In backoff until %s (%.0fs interval)", b.BackoffUntil.Format("15:04:05"), b.BackoffSeconds)
		return staleCacheOrNil(cacheFile), subscription, tier, isApiBilling
	}

	// Acquire fetch lock so multiple sessions don't race
	lockFile := getCacheFile("usage.lock")
	lock, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		// Another session is fetching — check if the lock is stale (>30s)
		if info, statErr := os.Stat(lockFile); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			os.Remove(lockFile)
			config.DebugLog("Removed stale lock file")
		} else {
			config.DebugLog("Another session is fetching, using cache")
		}
		// Re-check cache (the other session may have just written it)
		if cache, valid := loadCache(cacheFile, cfg.CacheTTL); valid {
			return cache, subscription, tier, isApiBilling
		}
		return staleCacheOrNil(cacheFile), subscription, tier, isApiBilling
	}
	lock.Close()
	defer os.Remove(lockFile)

	// Re-check cache after acquiring lock (another session may have just fetched)
	if cache, valid := loadCache(cacheFile, cfg.CacheTTL); valid {
		if cache.ResetTime.IsZero() || !time.Now().After(cache.ResetTime) {
			config.DebugLog("Cache refreshed by another session: %.1f%%", cache.UsagePercent)
			return cache, subscription, tier, isApiBilling
		}
	}

	// Fetch from API
	usage, fetchErr := fetchUsage(creds)
	if fetchErr != nil {
		config.DebugLog("API error: %v", fetchErr)
		return staleCacheOrNil(cacheFile), subscription, tier, isApiBilling
	}

	// Success: decay backoff and save cache
	decayBackoff()
	saveCache(cacheFile, usage)
	config.DebugLog("Fetched usage: %.1f%%", usage.UsagePercent)
	return usage, subscription, tier, isApiBilling
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

// staleCacheOrNil returns expired cache data, clearing any values whose
// reset time has passed so we don't display stale "100% until" messages.
func staleCacheOrNil(cacheFile string) *types.UsageCache {
	cache, err := loadCacheIgnoreExpiry(cacheFile)
	if err != nil {
		return nil
	}
	if !cache.ResetTime.IsZero() && time.Now().After(cache.ResetTime) {
		config.DebugLog("Cache reset time has passed, clearing stale data")
		cache.UsagePercent = 0
		cache.ResetTime = time.Time{}
	}
	if !cache.SevenDayResetTime.IsZero() && time.Now().After(cache.SevenDayResetTime) {
		cache.SevenDayPercent = 0
		cache.SevenDayResetTime = time.Time{}
	}
	return cache
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

const (
	backoffMin     = 15 * time.Second
	backoffInitial = 30 * time.Second
	backoffMax     = 5 * time.Minute
)

type backoffState struct {
	BackoffUntil   time.Time `json:"backoff_until"`
	BackoffSeconds float64   `json:"backoff_seconds"`
}

func loadBackoff() *backoffState {
	data, err := os.ReadFile(getCacheFile("backoff.json"))
	if err != nil {
		return nil
	}
	var b backoffState
	if err := json.Unmarshal(data, &b); err != nil {
		return nil
	}
	return &b
}

func saveBackoff(b *backoffState) {
	data, _ := json.Marshal(b)
	os.WriteFile(getCacheFile("backoff.json"), data, 0644)
}

func clearBackoff() {
	os.Remove(getCacheFile("backoff.json"))
}

func increaseBackoff(retryAfterHeader string) {
	b := loadBackoff()

	// Use Retry-After header if valid
	if ra, err := strconv.Atoi(retryAfterHeader); err == nil && ra > 0 {
		dur := time.Duration(ra) * time.Second
		saveBackoff(&backoffState{
			BackoffUntil:   time.Now().Add(dur),
			BackoffSeconds: dur.Seconds(),
		})
		return
	}

	// Adaptive: 1.5x the previous backoff
	next := backoffInitial
	if b != nil {
		next = time.Duration(b.BackoffSeconds*1.5) * time.Second
	}
	if next < backoffInitial {
		next = backoffInitial
	}
	if next > backoffMax {
		next = backoffMax
	}
	saveBackoff(&backoffState{
		BackoffUntil:   time.Now().Add(next),
		BackoffSeconds: next.Seconds(),
	})
}

func decayBackoff() {
	b := loadBackoff()
	if b == nil {
		return
	}
	next := time.Duration(b.BackoffSeconds*0.8) * time.Second
	if next < backoffMin {
		clearBackoff()
		return
	}
	// Keep the reduced interval for next time, but don't block now
	saveBackoff(&backoffState{
		BackoffUntil:   time.Time{}, // not blocking, just remembering the level
		BackoffSeconds: next.Seconds(),
	})
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

	if resp.StatusCode == http.StatusTooManyRequests {
		increaseBackoff(resp.Header.Get("Retry-After"))
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var usageResp types.UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		return nil, err
	}

	// Extract both five_hour and seven_day windows
	if usageResp.FiveHour == nil {
		return nil, fmt.Errorf("no five_hour usage data")
	}

	resetTime, _ := time.Parse(time.RFC3339, usageResp.FiveHour.ResetsAt)

	cache := &types.UsageCache{
		UsagePercent: usageResp.FiveHour.Utilization,
		ResetTime:    resetTime,
	}

	// Add seven_day data if available
	if usageResp.SevenDay != nil {
		sevenDayResetTime, _ := time.Parse(time.RFC3339, usageResp.SevenDay.ResetsAt)
		cache.SevenDayPercent = usageResp.SevenDay.Utilization
		cache.SevenDayResetTime = sevenDayResetTime
	}

	return cache, nil
}
