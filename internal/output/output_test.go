package output

import (
	"strings"
	"testing"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
)

// Helper to create a test config and restore original after test
func withConfig(t *testing.T, cfg *config.Config, fn func()) {
	t.Helper()
	originalCfg := config.Get()
	defer func() { *config.Get() = *originalCfg }()
	*config.Get() = *cfg
	fn()
}

// TestFullStatusLine tests complete statusline with all components
func TestFullStatusLine(t *testing.T) {
	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	withConfig(t, cfg, func() {
		session := &types.SessionInput{
			Model: &types.SessionModel{
				ID:          "claude-sonnet-4-5-20250929",
				DisplayName: "Sonnet 4.5",
			},
		}

		gitInfo := types.GitInfo{
			IsRepo:       true,
			Branch:       "feature/test-branch",
			HasModified:  true,
			HasStaged:    true,
			HasUntracked: true,
			Ahead:        3,
			Behind:       1,
		}

		usage := &types.UsageCache{
			UsagePercent: 45.0,
			ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute),
		}

		stats := &types.TokenStats{
			DailyCost:   15.50,
			WeeklyCost:  89.25,
			MonthlyCost: 350.75,
		}

		result := FormatStatusLine(session, gitInfo, usage, stats, "pro", "max_5x")

		// Verify all parts are present
		checks := map[string]bool{
			"git branch":        strings.Contains(result, "feature/test-branch"),
			"modified (!):":     strings.Contains(result, "!"),
			"staged (+)":        strings.Contains(result, "+"),
			"untracked (?)":     strings.Contains(result, "?"),
			"ahead (↑3)":        strings.Contains(result, "↑3"),
			"behind (↓1)":       strings.Contains(result, "↓1"),
			"model name":        strings.Contains(result, "Sonnet 4.5"),
			"tier":              strings.Contains(result, "5x"),
			"subscription":      strings.Contains(result, "pro"),
			"monthly cost":      strings.Contains(result, "$350.75/m"),
			"weekly cost":       strings.Contains(result, "$89.25/w"),
			"daily cost":        strings.Contains(result, "$15.50/d"),
			"usage percent":     strings.Contains(result, "45%"),
			"remaining time":    strings.Contains(result, "2h2") || strings.Contains(result, "2h3"), // Allow 2h29m or 2h30m
			"separator (|)":     strings.Contains(result, "|"),
		}

		for check, passed := range checks {
			if !passed {
				t.Errorf("Missing %s in output: %q", check, result)
			}
		}
	})
}

// TestGitStates tests various git repository states
func TestGitStates(t *testing.T) {
	tests := []struct {
		name     string
		gitInfo  types.GitInfo
		contains []string
		notContains []string
	}{
		{
			name: "clean repo",
			gitInfo: types.GitInfo{
				IsRepo: true,
				Branch: "main",
			},
			contains: []string{"main"},
			notContains: []string{"!", "+", "?", "↑", "↓"},
		},
		{
			name: "dirty repo with all indicators",
			gitInfo: types.GitInfo{
				IsRepo:       true,
				Branch:       "develop",
				HasModified:  true,
				HasStaged:    true,
				HasUntracked: true,
			},
			contains: []string{"develop", "!", "+", "?"},
		},
		{
			name: "ahead and behind",
			gitInfo: types.GitInfo{
				IsRepo: true,
				Branch: "main",
				Ahead:  5,
				Behind: 2,
			},
			contains: []string{"↑5", "↓2"},
		},
		{
			name: "only ahead",
			gitInfo: types.GitInfo{
				IsRepo: true,
				Branch: "main",
				Ahead:  10,
			},
			contains: []string{"↑10"},
			notContains: []string{"↓"},
		},
		{
			name: "not a git repo",
			gitInfo: types.GitInfo{
				IsRepo: false,
			},
			notContains: []string{"main", "!", "+", "?"},
		},
	}

	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfig(t, cfg, func() {
				result := FormatStatusLine(nil, tt.gitInfo, nil, &types.TokenStats{}, "", "")

				for _, want := range tt.contains {
					if !strings.Contains(result, want) {
						t.Errorf("Expected to contain %q, got: %q", want, result)
					}
				}

				for _, notWant := range tt.notContains {
					if strings.Contains(result, notWant) {
						t.Errorf("Expected NOT to contain %q, got: %q", notWant, result)
					}
				}
			})
		})
	}
}

// TestUsageStates tests various API usage scenarios
func TestUsageStates(t *testing.T) {
	tests := []struct {
		name     string
		usage    *types.UsageCache
		contains []string
		notContains []string
	}{
		{
			name: "normal usage on track",
			usage: &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute), // 50% elapsed
			},
			contains: []string{"50%", "2h"}, // Check for hour component (2h29m or 2h30m)
			notContains: []string{"↑", "↓", "until"},
		},
		{
			name: "usage trending over",
			usage: &types.UsageCache{
				UsagePercent: 65.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute), // 50% elapsed, expect ~50%
			},
			contains: []string{"65%", "↑"},
			notContains: []string{"↓"},
		},
		{
			name: "usage trending under",
			usage: &types.UsageCache{
				UsagePercent: 20.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute), // 50% elapsed, expect ~50%
			},
			contains: []string{"20%", "↓"},
			notContains: []string{"↑"},
		},
		{
			name: "at 100% shows reset time",
			usage: &types.UsageCache{
				UsagePercent: 100.0,
				ResetTime:    time.Date(2025, 12, 3, 15, 30, 0, 0, time.Local),
			},
			contains: []string{"100%", "until", "15:30"},
			notContains: []string{"↑", "↓"},
		},
		{
			name: "high usage warning (90%+)",
			usage: &types.UsageCache{
				UsagePercent: 95.0,
				ResetTime:    time.Now().Add(30 * time.Minute),
			},
			contains: []string{"95%"},
		},
		{
			name: "no usage data",
			usage: nil,
			notContains: []string{"%", "until"},
		},
		{
			name: "7-day window with normal usage",
			usage: &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute),
				SevenDayPercent: 25.0,
				SevenDayResetTime: time.Now().Add(3*24*time.Hour + 12*time.Hour),
			},
			contains: []string{"50%", "25%", "3d"},
		},
		{
			name: "7-day window trending over",
			usage: &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute),
				SevenDayPercent: 80.0,
				SevenDayResetTime: time.Now().Add(3*24*time.Hour + 12*time.Hour), // 50% elapsed, expect ~50%
			},
			contains: []string{"80%", "↑", "3d"},
		},
		{
			name: "7-day window at 100%",
			usage: &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Now().Add(2*time.Hour + 30*time.Minute),
				SevenDayPercent: 100.0,
				SevenDayResetTime: time.Date(2025, 12, 15, 14, 30, 0, 0, time.Local),
			},
			contains: []string{"100%", "until", "Dec 15"},
		},
	}

	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfig(t, cfg, func() {
				result := FormatStatusLine(nil, types.GitInfo{}, tt.usage, &types.TokenStats{}, "", "")

				for _, want := range tt.contains {
					if !strings.Contains(result, want) {
						t.Errorf("Expected to contain %q, got: %q", want, result)
					}
				}

				for _, notWant := range tt.notContains {
					if strings.Contains(result, notWant) {
						t.Errorf("Expected NOT to contain %q, got: %q", notWant, result)
					}
				}
			})
		})
	}
}

// TestCostScenarios tests various cost data scenarios
func TestCostScenarios(t *testing.T) {
	tests := []struct {
		name     string
		stats    *types.TokenStats
		contains []string
		notContains []string
	}{
		{
			name: "all costs present",
			stats: &types.TokenStats{
				DailyCost:   15.50,
				WeeklyCost:  89.25,
				MonthlyCost: 350.75,
			},
			contains: []string{"$15.50/d", "$89.25/w", "$350.75/m"},
		},
		{
			name: "only daily cost",
			stats: &types.TokenStats{
				DailyCost: 5.25,
			},
			contains: []string{"$5.25/d"},
		},
		{
			name: "zero costs",
			stats: &types.TokenStats{
				DailyCost:   0,
				WeeklyCost:  0,
				MonthlyCost: 0,
			},
			notContains: []string{"$"},
		},
		{
			name: "high monthly cost",
			stats: &types.TokenStats{
				MonthlyCost: 1234.56,
			},
			contains: []string{"$1234.56/m"},
		},
	}

	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfig(t, cfg, func() {
				result := FormatStatusLine(nil, types.GitInfo{}, nil, tt.stats, "", "")

				for _, want := range tt.contains {
					if !strings.Contains(result, want) {
						t.Errorf("Expected to contain %q, got: %q", want, result)
					}
				}

				for _, notWant := range tt.notContains {
					if strings.Contains(result, notWant) {
						t.Errorf("Expected NOT to contain %q, got: %q", notWant, result)
					}
				}
			})
		})
	}
}

// TestModelVariations tests different model input scenarios
func TestModelVariations(t *testing.T) {
	tests := []struct {
		name     string
		session  *types.SessionInput
		contains string
	}{
		{
			name: "with display name",
			session: &types.SessionInput{
				Model: &types.SessionModel{
					ID:          "claude-sonnet-4-5-20250929",
					DisplayName: "Sonnet 4.5",
				},
			},
			contains: "Sonnet 4.5",
		},
		{
			name: "without display name - formatted from ID",
			session: &types.SessionInput{
				Model: &types.SessionModel{
					ID: "claude-opus-4-1-20250514",
				},
			},
			contains: "opus.4.1",
		},
		{
			name: "haiku model",
			session: &types.SessionInput{
				Model: &types.SessionModel{
					ID: "claude-haiku-3-5",
				},
			},
			contains: "haiku.3.5",
		},
	}

	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfig(t, cfg, func() {
				result := FormatStatusLine(tt.session, types.GitInfo{}, nil, &types.TokenStats{}, "", "")
				if !strings.Contains(result, tt.contains) {
					t.Errorf("Expected to contain %q, got: %q", tt.contains, result)
				}
			})
		})
	}
}

// TestSubscriptionTierCombinations tests subscription and tier display
func TestSubscriptionTierCombinations(t *testing.T) {
	tests := []struct {
		name         string
		subscription string
		tier         string
		contains     string
	}{
		{
			name:         "both subscription and tier",
			subscription: "pro",
			tier:         "max_5x",
			contains:     "pro/5x",
		},
		{
			name:         "only tier",
			subscription: "",
			tier:         "default_claude_max_10x",
			contains:     "10x",
		},
		{
			name:         "only subscription",
			subscription: "team",
			tier:         "",
			contains:     "team",
		},
		{
			name:         "tier with complex format",
			subscription: "",
			tier:         "tier_2",
			contains:     "t2",
		},
	}

	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfig(t, cfg, func() {
				result := FormatStatusLine(nil, types.GitInfo{}, nil, &types.TokenStats{}, tt.subscription, tt.tier)
				if !strings.Contains(result, tt.contains) {
					t.Errorf("Expected to contain %q, got: %q", tt.contains, result)
				}
			})
		})
	}
}

// TestDisplayModes tests all display mode variations
func TestDisplayModes(t *testing.T) {
	session := &types.SessionInput{
		Model: &types.SessionModel{
			ID:          "claude-sonnet-4-5-20250929",
			DisplayName: "Sonnet 4.5",
		},
	}

	gitInfo := types.GitInfo{
		IsRepo: true,
		Branch: "main",
	}

	tests := []struct {
		name        string
		displayMode string
		noColor     bool
		checkANSI   bool
	}{
		{
			name:        "colors mode with ANSI",
			displayMode: "colors",
			noColor:     false,
			checkANSI:   true,
		},
		{
			name:        "minimal mode with ANSI",
			displayMode: "minimal",
			noColor:     false,
			checkANSI:   true,
		},
		{
			name:        "background mode with ANSI",
			displayMode: "background",
			noColor:     false,
			checkANSI:   true,
		},
		{
			name:        "no color mode",
			displayMode: "colors",
			noColor:     true,
			checkANSI:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				NoColor:     tt.noColor,
				DisplayMode: tt.displayMode,
				InfoMode:    "none",
			}

			withConfig(t, cfg, func() {
				result := FormatStatusLine(session, gitInfo, nil, &types.TokenStats{}, "", "")

				if result == "" {
					t.Error("Expected non-empty output")
				}

				// Check for ANSI codes
				hasANSI := strings.Contains(result, "\033[")
				if tt.checkANSI && !hasANSI {
					t.Error("Expected ANSI color codes but found none")
				}
				if !tt.checkANSI && hasANSI {
					t.Error("Expected no ANSI color codes but found some")
				}

				// Content should still be present
				if !strings.Contains(result, "main") {
					t.Error("Expected git branch 'main'")
				}
				if !strings.Contains(result, "Sonnet 4.5") {
					t.Error("Expected model name")
				}
			})
		})
	}
}

// TestInfoModes tests emoji and text prefix modes
func TestInfoModes(t *testing.T) {
	gitInfo := types.GitInfo{
		IsRepo: true,
		Branch: "main",
	}

	tests := []struct {
		name     string
		infoMode string
		contains []string
	}{
		{
			name:     "none - no prefixes",
			infoMode: "none",
			contains: []string{},
		},
		{
			name:     "emoji mode",
			infoMode: "emoji",
			contains: []string{"📁", "🔀"},
		},
		{
			name:     "text mode",
			infoMode: "text",
			contains: []string{"Dir:", "Git:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				NoColor:     true,
				DisplayMode: "colors",
				InfoMode:    tt.infoMode,
			}

			withConfig(t, cfg, func() {
				result := FormatStatusLine(nil, gitInfo, nil, &types.TokenStats{}, "", "")

				for _, want := range tt.contains {
					if !strings.Contains(result, want) {
						t.Errorf("Expected to contain %q in mode %q, got: %q", want, tt.infoMode, result)
					}
				}
			})
		})
	}
}

// TestHelperFunctions tests individual helper functions
func TestFormatModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "sonnet.4.5"},
		{"claude-opus-4-1-20250514", "opus.4.1"},
		{"claude-haiku-3-5", "haiku.3.5"},
		{"claude-sonnet", "sonnet"},
		{"claude-sonnet-3-5-20240229", "sonnet.3.5"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := formatModelName(tt.input)
			if result != tt.expected {
				t.Errorf("formatModelName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShortenTier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"default_claude_max_5x", "5x"},
		{"tier_10x", "10x"},
		{"tier_2", "t2"},
		{"max_15x", "15x"},
		{"MAX_5X", "5x"},
		{"tier_3", "t3"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shortenTier(tt.input)
			if result != tt.expected {
				t.Errorf("shortenTier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{3*time.Hour + 25*time.Minute, "3h25m"},
		{45 * time.Minute, "45m"},
		{2 * time.Hour, "2h0m"},
		{0, "0m"},
		{-1 * time.Hour, "0m"},
		{5*time.Hour + 0*time.Minute, "5h0m"},
		{1 * time.Minute, "1m"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatDurationDays(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{3*24*time.Hour + 22*time.Hour, "3d22h"},
		{1*24*time.Hour + 5*time.Hour, "1d5h"},
		{7*24*time.Hour + 0*time.Hour, "7d0h"},
		{0*24*time.Hour + 23*time.Hour + 45*time.Minute, "23h45m"},
		{0*24*time.Hour + 4*time.Hour + 20*time.Minute, "4h20m"},
		{45 * time.Minute, "45m"},
		{0, "0m"},
		{-1 * time.Hour, "0m"},
		{10*24*time.Hour + 12*time.Hour, "10d12h"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDurationDays(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDurationDays(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestCalculateProjection(t *testing.T) {
	tests := []struct {
		name         string
		usagePercent float64
		remaining    time.Duration
		expectArrow  bool
		expectUp     bool
	}{
		{
			name:         "exactly on track",
			usagePercent: 50.0,
			remaining:    2*time.Hour + 30*time.Minute, // 50% elapsed
			expectArrow:  false,
		},
		{
			name:         "trending significantly over",
			usagePercent: 60.0,
			remaining:    2*time.Hour + 30*time.Minute, // 50% elapsed, expect 50%
			expectArrow:  true,
			expectUp:     true,
		},
		{
			name:         "trending significantly under",
			usagePercent: 25.0,
			remaining:    2*time.Hour + 30*time.Minute, // 50% elapsed, expect 50%
			expectArrow:  true,
			expectUp:     false,
		},
		{
			name:         "slightly over but within 5% threshold",
			usagePercent: 52.0,
			remaining:    2*time.Hour + 30*time.Minute, // expect 50%, range [47.5-52.5]
			expectArrow:  false,
		},
		{
			name:         "slightly under but within 5% threshold",
			usagePercent: 48.0,
			remaining:    2*time.Hour + 30*time.Minute, // expect 50%, range [47.5-52.5]
			expectArrow:  false,
		},
		{
			name:         "at 100% - no projection shown",
			usagePercent: 100.0,
			remaining:    1 * time.Hour,
			expectArrow:  false,
		},
		{
			name:         "very early in window",
			usagePercent: 5.0,
			remaining:    4*time.Hour + 45*time.Minute, // 5% elapsed
			expectArrow:  false,
		},
		{
			name:         "late in window trending over",
			usagePercent: 95.0,
			remaining:    30 * time.Minute, // 90% elapsed, expect 90%
			expectArrow:  true, // 95 is outside 5% of 90 (85.5-94.5), 95 > 94.5
			expectUp:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTime := time.Now().Add(tt.remaining)
			result := calculateProjection(tt.usagePercent, resetTime, 5*time.Hour)

			if tt.expectArrow {
				if result == "" {
					t.Errorf("Expected arrow, got empty string (usage: %.1f%%, remaining: %v)",
						tt.usagePercent, tt.remaining)
				}
				if tt.expectUp && !strings.Contains(result, "↑") {
					t.Errorf("Expected ↑, got %q", result)
				}
				if !tt.expectUp && !strings.Contains(result, "↓") {
					t.Errorf("Expected ↓, got %q", result)
				}
			} else {
				if result != "" {
					t.Errorf("Expected no arrow, got %q (usage: %.1f%%, remaining: %v)",
						result, tt.usagePercent, tt.remaining)
				}
			}
		})
	}
}

// TestEdgeCases tests various edge cases and error conditions
func TestEdgeCases(t *testing.T) {
	cfg := &config.Config{
		NoColor:     true,
		DisplayMode: "colors",
		InfoMode:    "none",
	}

	t.Run("all nil inputs", func(t *testing.T) {
		withConfig(t, cfg, func() {
			result := FormatStatusLine(nil, types.GitInfo{}, nil, &types.TokenStats{}, "", "")
			// Should at least contain directory
			if result == "" {
				t.Error("Expected non-empty result with all nil inputs")
			}
		})
	})

	t.Run("session with nil model", func(t *testing.T) {
		withConfig(t, cfg, func() {
			session := &types.SessionInput{Model: nil}
			result := FormatStatusLine(session, types.GitInfo{}, nil, &types.TokenStats{}, "", "")
			if result == "" {
				t.Error("Expected non-empty result")
			}
		})
	})

	t.Run("usage with zero reset time", func(t *testing.T) {
		withConfig(t, cfg, func() {
			usage := &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Time{},
			}
			result := FormatStatusLine(nil, types.GitInfo{}, usage, &types.TokenStats{}, "", "")
			// Should show percentage but no time
			if !strings.Contains(result, "50%") {
				t.Error("Expected usage percentage")
			}
		})
	})

	t.Run("negative remaining time", func(t *testing.T) {
		withConfig(t, cfg, func() {
			usage := &types.UsageCache{
				UsagePercent: 50.0,
				ResetTime:    time.Now().Add(-1 * time.Hour), // In the past
			}
			result := FormatStatusLine(nil, types.GitInfo{}, usage, &types.TokenStats{}, "", "")
			// Should not crash
			if result == "" {
				t.Error("Expected non-empty result")
			}
		})
	})

	t.Run("very long branch name", func(t *testing.T) {
		withConfig(t, cfg, func() {
			gitInfo := types.GitInfo{
				IsRepo: true,
				Branch: "feature/very-long-branch-name-with-many-characters-that-goes-on-and-on",
			}
			result := FormatStatusLine(nil, gitInfo, nil, &types.TokenStats{}, "", "")
			if !strings.Contains(result, "feature/very-long-branch-name") {
				t.Error("Expected branch name in output")
			}
		})
	})
}
