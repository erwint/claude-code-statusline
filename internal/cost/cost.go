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

// SetEmbeddedPricing sets the embedded pricing data from main
func SetEmbeddedPricing(data []byte) {
	embeddedPricing = data
}

// GetTokenStats calculates cost statistics from log files
func GetTokenStats() *types.TokenStats {
	stats := &types.TokenStats{}
	pricing := loadPricing()
	seen := make(map[string]bool)

	now := time.Now()
	dailyCutoff := now.AddDate(0, 0, -1)
	weeklyCutoff := now.AddDate(0, 0, -7)
	monthlyCutoff := now.AddDate(0, -1, 0)

	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")

	config.DebugLog("Scanning logs from: %s", projectsDir)

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
			var entry types.LogEntry
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
			cost := calculateCost(entry.Message.Model, inputTokens, outputTokens, cacheCreation, cacheRead, pricing)

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

	config.DebugLog("Cost stats: daily=$%.2f, weekly=$%.2f, monthly=$%.2f",
		stats.DailyCost, stats.WeeklyCost, stats.MonthlyCost)

	return stats
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
		// Find last version number pattern
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
// "claude-sonnet-4-5" -> "claude-sonnet"
// "claude-opus-4" -> "claude-opus"
func stripVersion(model string) string {
	parts := strings.Split(model, "-")
	var result []string
	for _, part := range parts {
		// Skip numeric parts (version numbers)
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
		// No cache, try to fetch in background
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
