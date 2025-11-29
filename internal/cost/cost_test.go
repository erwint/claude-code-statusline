package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erwint/claude-code-statusline/internal/types"
)

func TestCalculateCost(t *testing.T) {
	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-opus-4-5":   {Input: 15.0, Output: 75.0},
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}

	tests := []struct {
		name          string
		model         string
		inputTokens   int
		outputTokens  int
		cacheCreation int
		cacheRead     int
		expectedCost  float64
	}{
		{
			name:         "simple input/output",
			model:        "claude-sonnet-4-5",
			inputTokens:  1000000,
			outputTokens: 1000000,
			expectedCost: 3.0 + 15.0, // $3 input + $15 output
		},
		{
			name:          "with cache creation",
			model:         "claude-sonnet-4-5",
			inputTokens:   1000000,
			outputTokens:  0,
			cacheCreation: 1000000,
			expectedCost:  3.0 + (3.0 * 1.25), // $3 input + $3.75 cache creation
		},
		{
			name:         "with cache read",
			model:        "claude-sonnet-4-5",
			inputTokens:  0,
			outputTokens: 0,
			cacheRead:    1000000,
			expectedCost: 0.3, // $0.30 cache read (3.0 * 0.1)
		},
		{
			name:         "opus model",
			model:        "claude-opus-4-5",
			inputTokens:  1000000,
			outputTokens: 1000000,
			expectedCost: 15.0 + 75.0, // $15 input + $75 output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := calculateCost(tt.model, tt.inputTokens, tt.outputTokens, tt.cacheCreation, tt.cacheRead, pricing)
			if !floatEquals(cost, tt.expectedCost) {
				t.Errorf("expected cost %.6f, got %.6f", tt.expectedCost, cost)
			}
		})
	}
}

// floatEquals compares two floats with a small tolerance for floating point precision
func floatEquals(a, b float64) bool {
	const epsilon = 0.0001
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

func TestGetPricingFallback(t *testing.T) {
	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-opus":       {Input: 15.0, Output: 75.0},
			"claude-sonnet":     {Input: 3.0, Output: 15.0},
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}

	tests := []struct {
		name          string
		model         string
		expectedInput float64
	}{
		{"exact match", "claude-sonnet-4-5", 3.0},
		{"strip date suffix", "claude-sonnet-4-5-20251101", 3.0},
		{"fallback to base", "claude-opus-4-5-20251101", 15.0},
		{"unknown model fallback", "claude-unknown-model", 3.0}, // default sonnet
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := getPricing(tt.model, pricing)
			if p.Input != tt.expectedInput {
				t.Errorf("expected input price %.2f, got %.2f", tt.expectedInput, p.Input)
			}
		})
	}
}

func TestStripVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5", "claude-sonnet"},
		{"claude-opus-4", "claude-opus"},
		{"claude-haiku-3-5", "claude-haiku"},
		{"claude-sonnet", "claude-sonnet"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := stripVersion(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestCostCacheLoadSave(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "cost_cache.json")

	// Create and save cache
	cache := &CostCache{
		DayCosts: map[string]float64{
			"2025-11-28": 10.50,
			"2025-11-29": 25.00,
		},
		FileState: map[string]FileProcessState{
			"/path/to/file.jsonl": {
				ModTime: time.Now(),
				Size:    1000,
				Offset:  500,
			},
		},
		ProcessedMessages: map[string]bool{
			"msg1:req1": true,
			"msg2:req2": true,
		},
	}

	saveCostCache(cacheFile, cache)

	// Load and verify
	loaded := loadCostCache(cacheFile)

	if len(loaded.DayCosts) != 2 {
		t.Errorf("expected 2 day costs, got %d", len(loaded.DayCosts))
	}
	if loaded.DayCosts["2025-11-28"] != 10.50 {
		t.Errorf("expected 10.50, got %.2f", loaded.DayCosts["2025-11-28"])
	}
	if len(loaded.ProcessedMessages) != 2 {
		t.Errorf("expected 2 processed messages, got %d", len(loaded.ProcessedMessages))
	}
}

func TestCleanupOldDays(t *testing.T) {
	cache := &CostCache{
		DayCosts: map[string]float64{
			"2025-10-01": 5.0,  // older than cutoff
			"2025-11-15": 10.0, // within range
			"2025-11-28": 20.0, // within range
		},
		ProcessedMessages: make(map[string]bool),
	}

	cutoff := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	cleanupOldDays(cache, cutoff)

	if len(cache.DayCosts) != 2 {
		t.Errorf("expected 2 days after cleanup, got %d", len(cache.DayCosts))
	}
	if _, exists := cache.DayCosts["2025-10-01"]; exists {
		t.Error("old day should have been removed")
	}
}

func TestAggregateStats(t *testing.T) {
	now := time.Date(2025, 11, 29, 12, 0, 0, 0, time.UTC)

	cache := &CostCache{
		DayCosts: map[string]float64{
			"2025-11-29": 50.0,  // today
			"2025-11-28": 30.0,  // yesterday (within daily)
			"2025-11-25": 20.0,  // 4 days ago (within weekly)
			"2025-11-20": 15.0,  // 9 days ago (within monthly, outside weekly)
			"2025-11-01": 10.0,  // within monthly
			"2025-10-15": 100.0, // should not be counted (older than 1 month)
		},
	}

	stats := aggregateStats(cache, now)

	// Daily: only today (2025-11-29)
	expectedDaily := 50.0
	if stats.DailyCost != expectedDaily {
		t.Errorf("expected daily cost %.2f, got %.2f", expectedDaily, stats.DailyCost)
	}

	// Weekly: 2025-11-22 to 2025-11-29
	expectedWeekly := 50.0 + 30.0 + 20.0
	if stats.WeeklyCost != expectedWeekly {
		t.Errorf("expected weekly cost %.2f, got %.2f", expectedWeekly, stats.WeeklyCost)
	}

	// Monthly: all in cache (cleanupOldDays should have removed 2025-10-15 before this)
	expectedMonthly := 50.0 + 30.0 + 20.0 + 15.0 + 10.0 + 100.0
	if stats.MonthlyCost != expectedMonthly {
		t.Errorf("expected monthly cost %.2f, got %.2f", expectedMonthly, stats.MonthlyCost)
	}
}

func TestProcessLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.jsonl")

	// Create test log entries
	entries := []map[string]interface{}{
		{
			"timestamp": "2025-11-29T10:00:00Z",
			"type":      "assistant",
			"message": map[string]interface{}{
				"id":    "msg1",
				"model": "claude-sonnet-4-5",
				"usage": map[string]int{
					"input_tokens":  1000,
					"output_tokens": 500,
				},
			},
			"requestId": "req1",
		},
		{
			"timestamp": "2025-11-29T11:00:00Z",
			"type":      "assistant",
			"message": map[string]interface{}{
				"id":    "msg2",
				"model": "claude-sonnet-4-5",
				"usage": map[string]int{
					"input_tokens":  2000,
					"output_tokens": 1000,
				},
			},
			"requestId": "req2",
		},
		{
			"timestamp": "2025-11-29T12:00:00Z",
			"type":      "user", // should be skipped
			"message":   map[string]interface{}{},
		},
	}

	// Write log file
	f, _ := os.Create(logFile)
	for _, entry := range entries {
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.Write([]byte("\n"))
	}
	f.Close()

	info, _ := os.Stat(logFile)
	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}

	monthlyCutoff := time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)

	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	// Check results
	if len(cache.ProcessedMessages) != 2 {
		t.Errorf("expected 2 processed messages, got %d", len(cache.ProcessedMessages))
	}

	dayCost := cache.DayCosts["2025-11-29"]
	// msg1: 1000 input ($0.003) + 500 output ($0.0075) = $0.0105
	// msg2: 2000 input ($0.006) + 1000 output ($0.015) = $0.021
	expectedCost := 0.0105 + 0.021
	if dayCost < expectedCost-0.001 || dayCost > expectedCost+0.001 {
		t.Errorf("expected day cost ~%.4f, got %.4f", expectedCost, dayCost)
	}
}

func TestProcessLogFileIncremental(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.jsonl")

	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}
	monthlyCutoff := time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)

	// Create initial log file with one entry
	f, _ := os.Create(logFile)
	entry1 := map[string]interface{}{
		"timestamp": "2025-11-29T10:00:00Z",
		"type":      "assistant",
		"message": map[string]interface{}{
			"id":    "msg1",
			"model": "claude-sonnet-4-5",
			"usage": map[string]int{"input_tokens": 1000, "output_tokens": 500},
		},
		"requestId": "req1",
	}
	data, _ := json.Marshal(entry1)
	f.Write(data)
	f.Write([]byte("\n"))
	f.Close()

	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	info, _ := os.Stat(logFile)
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	initialCost := cache.DayCosts["2025-11-29"]
	if len(cache.ProcessedMessages) != 1 {
		t.Errorf("expected 1 processed message after first run, got %d", len(cache.ProcessedMessages))
	}

	// Append new entry
	f, _ = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	entry2 := map[string]interface{}{
		"timestamp": "2025-11-29T11:00:00Z",
		"type":      "assistant",
		"message": map[string]interface{}{
			"id":    "msg2",
			"model": "claude-sonnet-4-5",
			"usage": map[string]int{"input_tokens": 2000, "output_tokens": 1000},
		},
		"requestId": "req2",
	}
	data, _ = json.Marshal(entry2)
	f.Write(data)
	f.Write([]byte("\n"))
	f.Close()

	// Process again - should only process new entry
	info, _ = os.Stat(logFile)
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	if len(cache.ProcessedMessages) != 2 {
		t.Errorf("expected 2 processed messages after second run, got %d", len(cache.ProcessedMessages))
	}

	newCost := cache.DayCosts["2025-11-29"]
	if newCost <= initialCost {
		t.Errorf("cost should have increased: initial=%.4f, new=%.4f", initialCost, newCost)
	}
}

func TestProcessLogFileDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.jsonl")

	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}
	monthlyCutoff := time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)

	// Create log file with duplicate entries (same message ID)
	f, _ := os.Create(logFile)
	for i := 0; i < 3; i++ {
		entry := map[string]interface{}{
			"timestamp": "2025-11-29T10:00:00Z",
			"type":      "assistant",
			"message": map[string]interface{}{
				"id":    "msg1", // same ID
				"model": "claude-sonnet-4-5",
				"usage": map[string]int{"input_tokens": 1000, "output_tokens": 500},
			},
			"requestId": "req1", // same request
		}
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.Write([]byte("\n"))
	}
	f.Close()

	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	info, _ := os.Stat(logFile)
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	// Should only count once despite 3 entries
	if len(cache.ProcessedMessages) != 1 {
		t.Errorf("expected 1 processed message (deduplicated), got %d", len(cache.ProcessedMessages))
	}

	// Cost should be for single message only
	expectedCost := (1000.0/1000000)*3.0 + (500.0/1000000)*15.0
	if cache.DayCosts["2025-11-29"] != expectedCost {
		t.Errorf("expected cost %.6f, got %.6f", expectedCost, cache.DayCosts["2025-11-29"])
	}
}

func TestDayOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.jsonl")

	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}
	monthlyCutoff := time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)

	// Create entries spanning multiple days
	f, _ := os.Create(logFile)
	days := []string{"2025-11-27", "2025-11-28", "2025-11-29"}
	for i, day := range days {
		entry := map[string]interface{}{
			"timestamp": day + "T12:00:00Z",
			"type":      "assistant",
			"message": map[string]interface{}{
				"id":    "msg" + string(rune('1'+i)),
				"model": "claude-sonnet-4-5",
				"usage": map[string]int{"input_tokens": 1000000, "output_tokens": 0},
			},
			"requestId": "req" + string(rune('1'+i)),
		}
		data, _ := json.Marshal(entry)
		f.Write(data)
		f.Write([]byte("\n"))
	}
	f.Close()

	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	info, _ := os.Stat(logFile)
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	// Each day should have $3.00
	for _, day := range days {
		if cache.DayCosts[day] != 3.0 {
			t.Errorf("expected $3.00 for %s, got $%.2f", day, cache.DayCosts[day])
		}
	}

	// Aggregate for 2025-11-29
	now := time.Date(2025, 11, 29, 18, 0, 0, 0, time.UTC)
	stats := aggregateStats(cache, now)

	// Daily: only 11-29 (today)
	if stats.DailyCost != 3.0 {
		t.Errorf("expected daily $3.00, got $%.2f", stats.DailyCost)
	}

	// Weekly: all 3 days
	if stats.WeeklyCost != 9.0 {
		t.Errorf("expected weekly $9.00, got $%.2f", stats.WeeklyCost)
	}

	// Monthly: all 3 days
	if stats.MonthlyCost != 9.0 {
		t.Errorf("expected monthly $9.00, got $%.2f", stats.MonthlyCost)
	}
}

func TestUnchangedFileSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.jsonl")

	pricing := &types.PricingData{
		Models: map[string]types.ModelPricing{
			"claude-sonnet-4-5": {Input: 3.0, Output: 15.0},
		},
	}
	monthlyCutoff := time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)

	// Create log file
	f, _ := os.Create(logFile)
	entry := map[string]interface{}{
		"timestamp": "2025-11-29T10:00:00Z",
		"type":      "assistant",
		"message": map[string]interface{}{
			"id":    "msg1",
			"model": "claude-sonnet-4-5",
			"usage": map[string]int{"input_tokens": 1000, "output_tokens": 500},
		},
		"requestId": "req1",
	}
	data, _ := json.Marshal(entry)
	f.Write(data)
	f.Write([]byte("\n"))
	f.Close()

	cache := &CostCache{
		DayCosts:          make(map[string]float64),
		FileState:         make(map[string]FileProcessState),
		ProcessedMessages: make(map[string]bool),
	}

	info, _ := os.Stat(logFile)
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)
	initialCost := cache.DayCosts["2025-11-29"]

	// Process again without changes
	processLogFile(logFile, info, cache, pricing, monthlyCutoff)

	// Cost should be unchanged (file was skipped)
	if cache.DayCosts["2025-11-29"] != initialCost {
		t.Errorf("cost changed when file was unchanged: %.4f -> %.4f", initialCost, cache.DayCosts["2025-11-29"])
	}
}
