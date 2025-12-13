package cost

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
)

const (
	pricingURL      = "https://raw.githubusercontent.com/erwint/claude-code-statusline/main/pricing.json"
	pricingCacheTTL = 24 * time.Hour
)

var embeddedPricing []byte

// CostCache stores per-day cost totals and file processing state
type CostCache struct {
	// DayCosts maps date string (YYYY-MM-DD) to total cost for that day
	DayCosts map[string]float64 `json:"day_costs"`
	// FileState tracks last processed position for each log file
	FileState map[string]FileProcessState `json:"file_state"`
	// ProcessedMessages tracks message IDs we've already counted
	ProcessedMessages map[string]bool `json:"processed_messages"`
}

// FileProcessState tracks processing state for a single log file
type FileProcessState struct {
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
	Offset  int64     `json:"offset"` // byte offset where we left off
}

// SetEmbeddedPricing sets the embedded pricing data from main
func SetEmbeddedPricing(data []byte) {
	embeddedPricing = data
}

// GetTokenStats calculates cost statistics from log files with caching
func GetTokenStats() *types.TokenStats {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "claude-code-statusline")
	cacheFile := filepath.Join(cacheDir, "cost_cache.json")
	lockFile := filepath.Join(cacheDir, "cost_cache.lock")

	// Ensure cache directory exists
	os.MkdirAll(cacheDir, 0755)

	// Acquire file lock for concurrent access protection
	lock, err := acquireLock(lockFile)
	if err != nil {
		config.DebugLog("Failed to acquire lock, proceeding without: %v", err)
	} else {
		defer releaseLock(lock)
	}

	cache := loadCostCache(cacheFile)
	pricing := loadPricing()

	now := time.Now()
	monthlyCutoff := now.AddDate(0, -1, 0)

	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	config.DebugLog("Scanning logs from: %s", projectsDir)

	// Clean up old days from cache (older than 31 days)
	cleanupOldDays(cache, monthlyCutoff)

	// Process log files
	filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		// Skip files older than monthly cutoff
		if info.ModTime().Before(monthlyCutoff) {
			return nil
		}

		processLogFile(path, info, cache, pricing, monthlyCutoff)
		return nil
	})

	// Save updated cache
	saveCostCache(cacheFile, cache)

	// Aggregate stats from daily buckets
	stats := aggregateStats(cache, now)

	config.DebugLog("Cost stats: daily=$%.2f, weekly=$%.2f, monthly=$%.2f",
		stats.DailyCost, stats.WeeklyCost, stats.MonthlyCost)

	return stats
}

func loadCostCache(path string) *CostCache {
	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}

	json.Unmarshal(data, cache)

	// Ensure maps are initialized
	if cache.DayCosts == nil {
		cache.DayCosts = make(map[string]float64)
	}
	if cache.FileState == nil {
		cache.FileState = make(map[string]FileProcessState)
	}
	if cache.ProcessedMessages == nil {
		cache.ProcessedMessages = make(map[string]bool)
	}

	return cache
}

func saveCostCache(path string, cache *CostCache) {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	data, err := json.Marshal(cache)
	if err != nil {
		config.DebugLog("Failed to marshal cost cache: %v", err)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		config.DebugLog("Failed to save cost cache: %v", err)
	}
}

func cleanupOldDays(cache *CostCache, cutoff time.Time) {
	cutoffStr := cutoff.Format("2006-01-02")
	for day := range cache.DayCosts {
		if day < cutoffStr {
			delete(cache.DayCosts, day)
		}
	}

	// Also clean up old message IDs (keep last 100k to prevent unbounded growth)
	if len(cache.ProcessedMessages) > 100000 {
		// Just clear it - we'll reprocess but that's fine
		cache.ProcessedMessages = make(map[string]bool)
		cache.FileState = make(map[string]FileProcessState)
		config.DebugLog("Cleared message cache (exceeded 100k entries)")
	}
}

func processLogFile(path string, info os.FileInfo, cache *CostCache, pricing *types.PricingData, monthlyCutoff time.Time) {
	state, exists := cache.FileState[path]

	// Check if file has changed since last processing
	if exists && state.ModTime.Equal(info.ModTime()) && state.Size == info.Size() {
		// File unchanged, skip
		config.DebugLog("Skipping unchanged file: %s", filepath.Base(path))
		return
	}

	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	var offset int64 = 0

	// If file grew, seek to last position (don't require modtime change - active files may buffer writes)
	if exists && state.Size < info.Size() {
		offset = state.Offset
		file.Seek(offset, 0)
		config.DebugLog("Resuming file %s from offset %d (was %d, now %d bytes)",
			filepath.Base(path), offset, state.Size, info.Size())
	} else if exists && state.Size > info.Size() {
		// File shrank (truncated or rewritten), reprocess from start
		config.DebugLog("File shrank, reprocessing from start: %s", filepath.Base(path))
	} else if exists {
		// File same size but modtime changed, reprocess from start
		config.DebugLog("Reprocessing modified file: %s", filepath.Base(path))
	}

	reader := bufio.NewReader(file)
	bytesRead := offset

	for {
		// ReadBytes automatically grows the buffer for large lines
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// Process last line if it doesn't end with newline
				if len(line) > 0 {
					bytesRead += int64(len(line))
					processLogEntry(line, cache, pricing, monthlyCutoff)
				}
				break
			}
			config.DebugLog("Read error for %s at offset %d: %v", filepath.Base(path), bytesRead, err)
			return
		}

		bytesRead += int64(len(line))
		processLogEntry(line, cache, pricing, monthlyCutoff)
	}

	// Update file state only if we successfully completed
	cache.FileState[path] = FileProcessState{
		ModTime: info.ModTime(),
		Size:    info.Size(),
		Offset:  bytesRead,
	}
}

func processLogEntry(line []byte, cache *CostCache, pricing *types.PricingData, monthlyCutoff time.Time) {
	// Note: For very large lines, json.Unmarshal will allocate memory temporarily,
	// but this is better than trying to parse across line boundaries with streaming.
	// bufio.Reader.ReadBytes automatically grows its buffer, so we can handle any line size.
	var entry types.LogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return
	}

	// Parse timestamp
	ts, err := time.Parse(time.RFC3339, entry.Timestamp)
	if err != nil || ts.Before(monthlyCutoff) {
		return
	}

	// Only process assistant messages with usage data
	if entry.Type != "assistant" {
		return
	}

	// Deduplicate by message ID + request ID
	key := entry.Message.ID + ":" + entry.RequestID
	if key == ":" || cache.ProcessedMessages[key] {
		return
	}
	cache.ProcessedMessages[key] = true

	// Get token counts
	inputTokens := entry.Message.Usage.InputTokens
	outputTokens := entry.Message.Usage.OutputTokens
	cacheCreation := entry.Message.Usage.CacheCreationInputTokens
	cacheRead := entry.Message.Usage.CacheReadInputTokens

	if inputTokens == 0 && outputTokens == 0 && cacheCreation == 0 && cacheRead == 0 {
		return
	}

	// Calculate cost
	cost := calculateCost(entry.Message.Model, inputTokens, outputTokens, cacheCreation, cacheRead, pricing)

	// Add to day bucket (use local time for user's perspective)
	day := ts.Local().Format("2006-01-02")
	cache.DayCosts[day] += cost
}

func aggregateStats(cache *CostCache, now time.Time) *types.TokenStats {
	cfg := config.Get()
	stats := &types.TokenStats{}

	if cfg.AggregationMode == "sliding" {
		// Sliding window: last 24h, last 7 days, last 30 days
		aggregateSliding(cache, now, stats)
	} else {
		// Fixed periods: today, this week, this month (default)
		aggregateFixed(cache, now, stats)
	}

	return stats
}

// aggregateSliding uses rolling windows: last 24h, 7d, 30d
func aggregateSliding(cache *CostCache, now time.Time, stats *types.TokenStats) {
	dailyCutoff := now.AddDate(0, 0, -1).Format("2006-01-02")
	weeklyCutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
	// Monthly cutoff already handled by cleanup

	for day, cost := range cache.DayCosts {
		stats.MonthlyCost += cost
		if day >= weeklyCutoff {
			stats.WeeklyCost += cost
		}
		if day >= dailyCutoff {
			stats.DailyCost += cost
		}
	}
}

// aggregateFixed uses calendar periods: today, this week (Mon-Sun), this month
func aggregateFixed(cache *CostCache, now time.Time, stats *types.TokenStats) {
	today := now.Format("2006-01-02")

	// Find start of week (Monday)
	weekday := now.Weekday()
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	weekStart := now.AddDate(0, 0, -int(weekday-1)).Format("2006-01-02")

	// Find start of month
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")

	for day, cost := range cache.DayCosts {
		if day >= monthStart {
			stats.MonthlyCost += cost
		}
		if day >= weekStart {
			stats.WeeklyCost += cost
		}
		if day == today {
			stats.DailyCost += cost
		}
	}
}

func calculateCost(model string, inputTokens, outputTokens, cacheCreation, cacheRead int, pricing *types.PricingData) float64 {
	p := getPricing(model, pricing)

	// Cache read tokens are discounted (10% of input price)
	// Cache creation tokens are charged at 1.25x input price
	var cost float64
	cost += float64(inputTokens) / 1000000 * p.Input
	cost += float64(cacheCreation) / 1000000 * p.Input * 1.25
	cost += float64(cacheRead) / 1000000 * p.Input * 0.1
	cost += float64(outputTokens) / 1000000 * p.Output
	return cost
}

// getPricing finds pricing for a model with fallback:
// 1. Exact match (e.g., "claude-sonnet-4-5-20250514")
// 2. Versioned model (e.g., "claude-sonnet-4-5")
// 3. Base model (e.g., "claude-sonnet")
// 4. Default sonnet pricing
func getPricing(model string, pricing *types.PricingData) types.ModelPricing {
	// Try exact match
	if p, ok := pricing.Models[model]; ok {
		return p
	}

	// Try without date suffix (e.g., "claude-sonnet-4-5-20250514" -> "claude-sonnet-4-5")
	if idx := strings.LastIndex(model, "-20"); idx > 0 {
		versionedModel := model[:idx]
		if p, ok := pricing.Models[versionedModel]; ok {
			return p
		}

		// Try base model (e.g., "claude-sonnet-4-5" -> "claude-sonnet")
		baseModel := stripVersion(versionedModel)
		if p, ok := pricing.Models[baseModel]; ok {
			return p
		}
	}

	// Try stripping version from original model
	baseModel := stripVersion(model)
	if p, ok := pricing.Models[baseModel]; ok {
		return p
	}

	// Default to sonnet pricing
	return types.ModelPricing{Input: 3.0, Output: 15.0}
}

// stripVersion removes version numbers from model name
func stripVersion(model string) string {
	parts := strings.Split(model, "-")
	var result []string
	for _, part := range parts {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			continue
		}
		result = append(result, part)
	}
	return strings.Join(result, "-")
}

func loadPricing() *types.PricingData {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "claude-code-statusline")
	cacheFile := filepath.Join(cacheDir, "pricing.json")

	// Check if cache exists and is fresh (< 24h old)
	if info, err := os.Stat(cacheFile); err == nil {
		if time.Since(info.ModTime()) < pricingCacheTTL {
			if data, err := os.ReadFile(cacheFile); err == nil {
				var pricing types.PricingData
				if json.Unmarshal(data, &pricing) == nil {
					config.DebugLog("Using cached pricing (age: %v)", time.Since(info.ModTime()))
					return &pricing
				}
			}
		} else {
			config.DebugLog("Pricing cache expired, fetching update...")
			go fetchAndCachePricing(cacheDir, cacheFile)
		}
	} else {
		config.DebugLog("No pricing cache, fetching...")
		go fetchAndCachePricing(cacheDir, cacheFile)
	}

	// Fall back to embedded pricing
	var pricing types.PricingData
	json.Unmarshal(embeddedPricing, &pricing)
	return &pricing
}

func fetchAndCachePricing(cacheDir, cacheFile string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(pricingURL)
	if err != nil {
		config.DebugLog("Failed to fetch pricing: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		config.DebugLog("Pricing fetch returned status %d", resp.StatusCode)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		config.DebugLog("Failed to read pricing response: %v", err)
		return
	}

	// Validate JSON before caching
	var pricing types.PricingData
	if err := json.Unmarshal(data, &pricing); err != nil {
		config.DebugLog("Invalid pricing JSON: %v", err)
		return
	}

	// Save to cache
	os.MkdirAll(cacheDir, 0755)
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		config.DebugLog("Failed to cache pricing: %v", err)
		return
	}

	config.DebugLog("Pricing updated and cached")
}
