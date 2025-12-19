package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
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

// FormatStatusLine builds the complete status line output
func FormatStatusLine(session *types.SessionInput, git types.GitInfo, usage *types.UsageCache, stats *types.TokenStats, subscription, tier string, isApiBilling bool) string {
	cfg := config.Get()
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
	parts = append(parts, colorize(dir, colorBlue, bgBlue, cfg))

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
		parts = append(parts, colorize(gitPart, colorMagenta, bgMagenta, cfg))
	}

	// Model info (from stdin session)
	if session != nil && session.Model != nil {
		modelName := session.Model.DisplayName
		if modelName == "" {
			modelName = formatModelName(session.Model.ID)
		}
		parts = append(parts, colorize(modelName, colorCyan, bgCyan, cfg))
	}

	// Subscription type with tier
	if subscription != "" || tier != "" {
		subPart := subscription
		if tier != "" {
			shortTier := shortenTier(tier)
			if subPart != "" {
				subPart += "/" + shortTier
			} else {
				subPart = shortTier
			}
		}
		parts = append(parts, colorize(subPart, colorGray, bgBlue, cfg))
	}

	// Cost breakdown: monthly / weekly / daily
	if stats.DailyCost > 0 || stats.WeeklyCost > 0 || stats.MonthlyCost > 0 {
		costPart := fmt.Sprintf("$%.2f/m $%.2f/w $%.2f/d",
			stats.MonthlyCost, stats.WeeklyCost, stats.DailyCost)
		parts = append(parts, colorize(costPart, colorCyan, bgCyan, cfg))
	}

	// API Usage info (at the end)
	if usage != nil {
		// 5-hour window
		usageColor := colorGreen
		usageBg := bgGreen

		// Grey out usage display when on API billing
		if isApiBilling {
			usageColor = colorGray
			usageBg = bgBlue
		} else if usage.UsagePercent >= 90 {
			usageColor = colorRed
			usageBg = bgRed
		} else if usage.UsagePercent >= 75 {
			usageColor = colorYellow
			usageBg = bgYellow
		}

		usagePart := fmt.Sprintf("%.0f%%", usage.UsagePercent)

		// Add projection arrow if significantly off track
		if !usage.ResetTime.IsZero() && usage.UsagePercent < 100 {
			projection := calculateProjection(usage.UsagePercent, usage.ResetTime, 5*time.Hour, usageColor)
			if projection != "" {
				usagePart += projection
			}
		}

		// Reset time
		if !usage.ResetTime.IsZero() {
			if usage.UsagePercent >= 100 {
				// At limit: show when it resets (local time)
				resetLocal := usage.ResetTime.Local()
				usagePart += fmt.Sprintf(" until %s", resetLocal.Format("15:04"))
			} else {
				// Not at limit: show time remaining
				remaining := time.Until(usage.ResetTime)
				if remaining > 0 {
					usagePart += " " + formatDuration(remaining)
				}
			}
		}

		parts = append(parts, colorize(usagePart, usageColor, usageBg, cfg))

		// 7-day window
		if usage.SevenDayPercent > 0 && !usage.SevenDayResetTime.IsZero() {
			sevenDayColor := colorGreen
			sevenDayBg := bgGreen

			// Grey out usage display when on API billing
			if isApiBilling {
				sevenDayColor = colorGray
				sevenDayBg = bgBlue
			} else if usage.SevenDayPercent >= 90 {
				sevenDayColor = colorRed
				sevenDayBg = bgRed
			} else if usage.SevenDayPercent >= 75 {
				sevenDayColor = colorYellow
				sevenDayBg = bgYellow
			}

			sevenDayPart := fmt.Sprintf("%.0f%%", usage.SevenDayPercent)

			// Add projection arrow for 7-day window
			if usage.SevenDayPercent < 100 {
				projection := calculateProjection(usage.SevenDayPercent, usage.SevenDayResetTime, 7*24*time.Hour, sevenDayColor)
				if projection != "" {
					sevenDayPart += projection
				}
			}

			// Reset time for 7-day window
			if usage.SevenDayPercent >= 100 {
				resetLocal := usage.SevenDayResetTime.Local()
				sevenDayPart += fmt.Sprintf(" until %s", resetLocal.Format("Jan 2 15:04"))
			} else {
				// Not at limit: show time remaining in days/hours format
				remaining := time.Until(usage.SevenDayResetTime)
				if remaining > 0 {
					sevenDayPart += " " + formatDurationDays(remaining)
				}
			}

			parts = append(parts, colorize(sevenDayPart, sevenDayColor, sevenDayBg, cfg))
		}
	}

	// Add info mode prefixes
	if cfg.InfoMode == "emoji" {
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
	} else if cfg.InfoMode == "text" {
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

func colorize(text, fgColor, bgColor string, cfg *config.Config) string {
	if cfg.NoColor {
		return text
	}

	switch cfg.DisplayMode {
	case "minimal":
		return colorGray + text + colorReset
	case "background":
		return bgColor + " " + text + " " + colorReset
	default: // colors
		return fgColor + text + colorReset
	}
}

func formatModelName(model string) string {
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

func formatDurationDays(d time.Duration) string {
	if d < 0 {
		return "0m"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}

	// Less than a day, use regular format
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func calculateProjection(usagePercent float64, resetTime time.Time, totalWindow time.Duration, baseColor string) string {
	// Don't show projection at 100% - we show reset time instead
	if usagePercent >= 100 {
		return ""
	}

	remaining := time.Until(resetTime)

	if remaining <= 0 {
		return ""
	}

	// Time elapsed = totalWindow - remaining
	elapsed := totalWindow - remaining

	if elapsed <= 0 || totalWindow <= 0 {
		return ""
	}

	// Expected usage at this point: elapsed / total * 100
	expectedPercent := (float64(elapsed) / float64(totalWindow)) * 100

	// Calculate deviation ranges
	lowerBound5 := expectedPercent * 0.95
	upperBound5 := expectedPercent * 1.05
	lowerBound25 := expectedPercent * 0.75
	upperBound25 := expectedPercent * 1.25

	// Determine arrow based on deviation
	var arrow string
	if usagePercent > upperBound25 {
		// >25% over: heavy arrow
		arrow = " ⬆"
	} else if usagePercent > upperBound5 {
		// 5-25% over: double line arrow
		arrow = " ⇈"
	} else if usagePercent < lowerBound25 {
		// >25% under: heavy arrow
		arrow = " ⬇"
	} else if usagePercent < lowerBound5 {
		// 5-25% under: double line arrow
		arrow = " ⇊"
	} else {
		// Within ±5%: on track, no arrow
		return ""
	}

	// Color the arrow
	if baseColor == colorGray {
		return arrow // Plain arrow, parent will colorize grey
	} else if usagePercent > upperBound5 {
		// Trending over: use red
		return " " + colorRed + strings.TrimSpace(arrow) + baseColor
	} else {
		// Trending under: use base color (green)
		return arrow
	}
}

func shortenTier(tier string) string {
	tier = strings.ToLower(tier)

	// Handle "Nx" patterns (e.g., "_5x", "_10x")
	for i := len(tier) - 1; i >= 1; i-- {
		if tier[i] == 'x' && tier[i-1] >= '0' && tier[i-1] <= '9' {
			start := i - 1
			for start > 0 && tier[start-1] >= '0' && tier[start-1] <= '9' {
				start--
			}
			return tier[start : i+1]
		}
	}

	// Handle "tier_N" patterns
	tier = strings.ReplaceAll(tier, "tier_", "t")
	tier = strings.ReplaceAll(tier, "tier", "t")

	// Remove common prefixes
	tier = strings.TrimPrefix(tier, "default_")
	tier = strings.TrimPrefix(tier, "claude_")

	return tier
}
