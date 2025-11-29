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

	// Try to find pricing patterns in HTML
	// Pattern: model name followed by input/output prices
	// This is a best-effort parser that may need adjustment

	// Look for patterns like "$3 / $15" or "$3.00 / $15.00" near model names
	modelPatterns := map[string][]string{
		"claude-opus-4-5":   {"opus 4.5", "opus-4.5", "claude opus 4.5"},
		"claude-sonnet-4-5": {"sonnet 4.5", "sonnet-4.5", "claude sonnet 4.5"},
		"claude-haiku-4-5":  {"haiku 4.5", "haiku-4.5", "claude haiku 4.5"},
		"claude-opus-4":     {"opus 4.1", "opus-4.1", "opus 4", "claude opus 4"},
		"claude-sonnet-4":   {"sonnet 4", "sonnet-4", "claude sonnet 4"},
		"claude-sonnet-3-7": {"sonnet 3.7", "sonnet-3.7", "claude sonnet 3.7"},
		"claude-haiku-3-5":  {"haiku 3.5", "haiku-3.5", "claude haiku 3.5"},
		"claude-3-opus":     {"opus 3", "claude-3-opus", "claude 3 opus"},
		"claude-3-sonnet":   {"sonnet 3", "claude-3-sonnet", "claude 3 sonnet"},
		"claude-3-haiku":    {"haiku 3", "claude-3-haiku", "claude 3 haiku"},
	}

	priceRegex := regexp.MustCompile(`\$(\d+(?:\.\d+)?)\s*(?:per|/)\s*(?:1M|MTok|million).*?\$(\d+(?:\.\d+)?)\s*(?:per|/)\s*(?:1M|MTok|million)`)
	simplePriceRegex := regexp.MustCompile(`\$(\d+(?:\.\d+)?)\s*/\s*\$(\d+(?:\.\d+)?)`)

	htmlLower := strings.ToLower(html)

	for modelID, patterns := range modelPatterns {
		for _, pattern := range patterns {
			idx := strings.Index(htmlLower, strings.ToLower(pattern))
			if idx == -1 {
				continue
			}

			// Look for pricing within 500 chars after the model name
			searchArea := html[idx:min(idx+500, len(html))]

			// Try detailed pattern first
			if matches := priceRegex.FindStringSubmatch(searchArea); len(matches) >= 3 {
				input, _ := strconv.ParseFloat(matches[1], 64)
				output, _ := strconv.ParseFloat(matches[2], 64)
				if input > 0 && output > 0 {
					pricing.Models[modelID] = ModelPricing{Input: input, Output: output}
					break
				}
			}

			// Try simple pattern
			if matches := simplePriceRegex.FindStringSubmatch(searchArea); len(matches) >= 3 {
				input, _ := strconv.ParseFloat(matches[1], 64)
				output, _ := strconv.ParseFloat(matches[2], 64)
				if input > 0 && output > 0 {
					pricing.Models[modelID] = ModelPricing{Input: input, Output: output}
					break
				}
			}
		}
	}

	return pricing
}

func getFallbackPricing() PricingData {
	return PricingData{
		Models: map[string]ModelPricing{
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
