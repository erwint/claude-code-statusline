//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PricingData struct {
	Updated string                  `json:"updated"`
	Models  map[string]ModelPricing `json:"models"`
}

type ModelPricing struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

func main() {
	fmt.Println("Fetching pricing from claude.com/pricing...")

	resp, err := http.Get("https://claude.com/pricing")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch pricing page: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read response: %v\n", err)
		os.Exit(1)
	}

	html := string(body)
	pricing := parsePricing(html)

	if len(pricing.Models) == 0 {
		fmt.Println("Warning: Could not parse any pricing data from page")
		fmt.Println("Using fallback pricing data...")
		pricing = getFallbackPricing()
	}

	pricing.Updated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(pricing, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("pricing.json", data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write pricing.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully updated pricing.json")
	fmt.Printf("Found %d models:\n", len(pricing.Models))
	for model, price := range pricing.Models {
		fmt.Printf("  %s: $%.2f input, $%.2f output\n", model, price.Input, price.Output)
	}
}

func parsePricing(html string) PricingData {
	pricing := PricingData{
		Models: make(map[string]ModelPricing),
	}

	// Auto-detect model names from page
	// Look for patterns like "Claude 3.5 Sonnet", "Opus 4.5", "Haiku 3", etc.
	modelRegex := regexp.MustCompile(`(?i)(claude\s+)?(\d+(?:\.\d+)?)\s*(opus|sonnet|haiku)|(opus|sonnet|haiku)\s*(\d+(?:\.\d+)?)`)

	// Price patterns: "$X.XX / MTok" or "$X / $Y" or "$X per million"
	// Matches input and output prices
	priceBlockRegex := regexp.MustCompile(`(?i)\$(\d+(?:\.\d+)?)\s*(?:/\s*(?:1M|MTok|million|M\s*tokens?)|\s*per\s*(?:million|1M|MTok))?\s*(?:input)?[^$]*\$(\d+(?:\.\d+)?)\s*(?:/\s*(?:1M|MTok|million|M\s*tokens?)|\s*per\s*(?:million|1M|MTok))?\s*(?:output)?`)

	// Also try simpler pattern "$X / $Y"
	simplePriceRegex := regexp.MustCompile(`\$(\d+(?:\.\d+)?)\s*/\s*\$(\d+(?:\.\d+)?)`)

	htmlLower := strings.ToLower(html)

	// Find all model mentions
	modelMatches := modelRegex.FindAllStringSubmatchIndex(htmlLower, -1)

	for _, match := range modelMatches {
		if match[0] < 0 {
			continue
		}

		modelStr := htmlLower[match[0]:match[1]]
		modelID := normalizeModelName(modelStr)
		if modelID == "" {
			continue
		}

		// Already have this model
		if _, exists := pricing.Models[modelID]; exists {
			continue
		}

		// Search for prices within 800 chars after the model name
		searchStart := match[0]
		searchEnd := min(match[1]+800, len(html))
		searchArea := html[searchStart:searchEnd]

		// Try to find price block
		var input, output float64

		if priceMatches := priceBlockRegex.FindStringSubmatch(searchArea); len(priceMatches) >= 3 {
			input, _ = strconv.ParseFloat(priceMatches[1], 64)
			output, _ = strconv.ParseFloat(priceMatches[2], 64)
		}

		// Fallback to simple pattern
		if input == 0 || output == 0 {
			if priceMatches := simplePriceRegex.FindStringSubmatch(searchArea); len(priceMatches) >= 3 {
				input, _ = strconv.ParseFloat(priceMatches[1], 64)
				output, _ = strconv.ParseFloat(priceMatches[2], 64)
			}
		}

		if input > 0 && output > 0 {
			pricing.Models[modelID] = ModelPricing{Input: input, Output: output}
			fmt.Printf("  Found: %s -> $%.2f / $%.2f\n", modelID, input, output)
		}
	}

	// Also look for JSON-LD or structured data that might contain pricing
	jsonPricing := extractJSONPricing(html)
	for id, price := range jsonPricing {
		if _, exists := pricing.Models[id]; !exists {
			pricing.Models[id] = price
		}
	}

	return pricing
}

func normalizeModelName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "claude ", "")
	name = strings.TrimSpace(name)

	// Extract model family and version
	var family, version string

	// Pattern: "3.5 sonnet" or "sonnet 3.5" or "opus 4.5"
	parts := strings.Fields(name)
	for _, part := range parts {
		switch part {
		case "opus", "sonnet", "haiku":
			family = part
		default:
			// Check if it's a version number
			if _, err := strconv.ParseFloat(part, 64); err == nil {
				version = part
			}
		}
	}

	if family == "" {
		return ""
	}

	// Build canonical model ID
	if version != "" {
		// Convert "3.5" to "3-5"
		version = strings.ReplaceAll(version, ".", "-")
		return fmt.Sprintf("claude-%s-%s", family, version)
	}

	return fmt.Sprintf("claude-%s", family)
}

func extractJSONPricing(html string) map[string]ModelPricing {
	result := make(map[string]ModelPricing)

	// Look for JSON blocks in script tags
	jsonRegex := regexp.MustCompile(`<script[^>]*type="application/(?:ld\+)?json"[^>]*>([\s\S]*?)</script>`)
	matches := jsonRegex.FindAllStringSubmatch(html, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		// Try to parse as JSON and look for pricing info
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(match[1]), &data); err != nil {
			continue
		}

		// Look for pricing structures (this is speculative)
		extractPricingFromJSON(data, result)
	}

	return result
}

func extractPricingFromJSON(data map[string]interface{}, result map[string]ModelPricing) {
	// Recursively search for pricing patterns in JSON
	for key, value := range data {
		keyLower := strings.ToLower(key)

		// Check if this looks like a model entry
		if strings.Contains(keyLower, "opus") || strings.Contains(keyLower, "sonnet") || strings.Contains(keyLower, "haiku") {
			if nested, ok := value.(map[string]interface{}); ok {
				var input, output float64
				if v, ok := nested["input"].(float64); ok {
					input = v
				}
				if v, ok := nested["output"].(float64); ok {
					output = v
				}
				if input > 0 && output > 0 {
					modelID := normalizeModelName(key)
					if modelID != "" {
						result[modelID] = ModelPricing{Input: input, Output: output}
					}
				}
			}
		}

		// Recurse into nested objects
		if nested, ok := value.(map[string]interface{}); ok {
			extractPricingFromJSON(nested, result)
		}
		if arr, ok := value.([]interface{}); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]interface{}); ok {
					extractPricingFromJSON(nested, result)
				}
			}
		}
	}
}

func getFallbackPricing() PricingData {
	return PricingData{
		Models: map[string]ModelPricing{
			// Latest models (4.5 series)
			"claude-opus-4-5":   {Input: 5.0, Output: 25.0},
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
			"claude-haiku-4-5":  {Input: 1.0, Output: 5.0},
			// 4.x series
			"claude-opus-4":   {Input: 15.0, Output: 75.0},
			"claude-sonnet-4": {Input: 3.0, Output: 15.0},
			// 3.x series
			"claude-sonnet-3-7": {Input: 3.0, Output: 15.0},
			"claude-haiku-3-5":  {Input: 0.8, Output: 4.0},
			"claude-opus-3":     {Input: 15.0, Output: 75.0},
			"claude-sonnet-3":   {Input: 3.0, Output: 15.0},
			"claude-haiku-3":    {Input: 0.25, Output: 1.25},
			// With date suffixes (for exact matching)
			"claude-opus-4-5-20250514":   {Input: 5.0, Output: 25.0},
			"claude-sonnet-4-5-20250514": {Input: 3.0, Output: 15.0},
			"claude-haiku-4-5-20250514":  {Input: 1.0, Output: 5.0},
			"claude-opus-4-20250514":     {Input: 15.0, Output: 75.0},
			"claude-sonnet-4-20250514":   {Input: 3.0, Output: 15.0},
			"claude-sonnet-3-7-20250219": {Input: 3.0, Output: 15.0},
			"claude-haiku-3-5-20241022":  {Input: 0.8, Output: 4.0},
			"claude-3-opus-20240229":     {Input: 15.0, Output: 75.0},
			"claude-3-sonnet-20240229":   {Input: 3.0, Output: 15.0},
			"claude-3-haiku-20240307":    {Input: 0.25, Output: 1.25},
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
