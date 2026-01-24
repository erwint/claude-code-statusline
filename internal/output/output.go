package output

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/session"
	"github.com/erwint/claude-code-statusline/internal/transcript"
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
func FormatStatusLine(sess *types.SessionInput, git types.GitInfo, usage *types.UsageCache, stats *types.TokenStats, subscription, tier string, isApiBilling bool, transcriptData *types.TranscriptData) string {
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
	if sess != nil && sess.Model != nil {
		modelName := sess.Model.DisplayName
		if modelName == "" {
			modelName = formatModelName(sess.Model.ID)
		}
		parts = append(parts, colorize(modelName, colorCyan, bgCyan, cfg))
	}

	// Context window usage bar
	if cfg.ShowContext && sess != nil && sess.ContextWindow != nil {
		contextPct := session.GetContextPercent(sess)
		if contextPct > 0 || sess.ContextWindow.Size > 0 {
			contextPart := formatContextBar(contextPct, cfg)
			parts = append(parts, contextPart)
		}
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

	// Add info mode prefixes to main status line
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

	// Build the main status line
	lines := []string{strings.Join(parts, " | ")}

	// Build the activity line (tools, agents, todos, duration)
	var activityParts []string

	// Tool activity
	if cfg.ShowTools && transcriptData != nil {
		toolPart := formatToolsActivity(transcriptData, cfg)
		if toolPart != "" {
			activityParts = append(activityParts, toolPart)
		}
	}

	// Agent activity
	if cfg.ShowAgents && transcriptData != nil {
		agentPart := formatAgentsActivity(transcriptData, cfg)
		if agentPart != "" {
			activityParts = append(activityParts, agentPart)
		}
	}

	// Todo progress
	if cfg.ShowTodos && transcriptData != nil {
		todoPart := formatTodoProgress(transcriptData, cfg)
		if todoPart != "" {
			activityParts = append(activityParts, todoPart)
		}
	}

	// Session duration
	if cfg.ShowDuration && transcriptData != nil {
		duration := transcript.GetSessionDuration(transcriptData)
		if duration != "" {
			activityParts = append(activityParts, colorize(duration, colorGray, bgBlue, cfg))
		}
	}

	// Add activity line if there's anything to show
	if len(activityParts) > 0 {
		lines = append(lines, strings.Join(activityParts, " | "))
	}

	return strings.Join(lines, "\n")
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
		// >25% over: wide headed arrow
		arrow = " ⮝"
	} else if usagePercent > upperBound5 {
		// 5-25% over: double line arrow
		arrow = " ⇈"
	} else if usagePercent < lowerBound25 {
		// >25% under: wide headed arrow
		arrow = " ⮟"
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

// formatContextBar renders a visual context window usage bar
func formatContextBar(percent float64, cfg *config.Config) string {
	const barWidth = 10

	// Determine color based on usage
	var fgColor, bgColor string
	if percent >= 85 {
		fgColor, bgColor = colorRed, bgRed
	} else if percent >= 70 {
		fgColor, bgColor = colorYellow, bgYellow
	} else {
		fgColor, bgColor = colorGreen, bgGreen
	}

	// Build the bar
	filled := int(percent / 100 * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	text := fmt.Sprintf("[%s] %.0f%%", bar, percent)

	return colorize(text, fgColor, bgColor, cfg)
}

// formatToolsActivity renders running and completed tools
func formatToolsActivity(data *types.TranscriptData, cfg *config.Config) string {
	if data == nil {
		return ""
	}

	var parts []string

	// Show running tools (up to 2)
	running := transcript.GetRunningTools(data)
	for i, tool := range running {
		if i >= 2 {
			break
		}
		toolStr := colorize("◐", colorYellow, bgYellow, cfg) + " " + colorize(tool.Name, colorCyan, bgCyan, cfg)
		if tool.Target != "" {
			toolStr += " " + colorize(tool.Target, colorGray, bgBlue, cfg)
		}
		parts = append(parts, toolStr)
	}

	// Show completed tool counts
	counts := transcript.GetCompletedToolCounts(data)
	if len(counts) > 0 {
		// Sort by count descending
		type toolCount struct {
			name  string
			count int
		}
		var sorted []toolCount
		for name, count := range counts {
			sorted = append(sorted, toolCount{name, count})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})

		// Show top 4
		var completedParts []string
		for i, tc := range sorted {
			if i >= 4 {
				break
			}
			if tc.count > 1 {
				completedParts = append(completedParts, fmt.Sprintf("%s×%d", tc.name, tc.count))
			} else {
				completedParts = append(completedParts, tc.name)
			}
		}

		if len(completedParts) > 0 {
			completedStr := colorize("✓", colorGreen, bgGreen, cfg) + " " + strings.Join(completedParts, ", ")
			parts = append(parts, completedStr)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " | ")
}

// formatAgentsActivity renders running agents
func formatAgentsActivity(data *types.TranscriptData, cfg *config.Config) string {
	if data == nil {
		return ""
	}

	running := transcript.GetRunningAgents(data)
	if len(running) == 0 {
		return ""
	}

	var parts []string
	for i, agent := range running {
		if i >= 2 {
			break
		}
		agentStr := colorize("◐", colorYellow, bgYellow, cfg) + " " + colorize(agent.Type, colorMagenta, bgMagenta, cfg)
		if agent.Description != "" {
			agentStr += ": " + colorize(agent.Description, colorGray, bgBlue, cfg)
		}
		// Show elapsed time
		elapsed := time.Since(agent.StartTime)
		if elapsed > 0 {
			agentStr += " " + colorize("("+formatShortDuration(elapsed)+")", colorGray, bgBlue, cfg)
		}
		parts = append(parts, agentStr)
	}

	return strings.Join(parts, " | ")
}

// formatTodoProgress renders todo progress
func formatTodoProgress(data *types.TranscriptData, cfg *config.Config) string {
	if data == nil {
		return ""
	}

	completed, total := transcript.GetTodoProgress(data)
	if total == 0 {
		return ""
	}

	progress := fmt.Sprintf("(%d/%d)", completed, total)

	// Check if all complete
	if completed == total {
		return colorize("✓", colorGreen, bgGreen, cfg) + " " + colorize("Done "+progress, colorGreen, bgGreen, cfg)
	}

	// Show current in-progress todo
	current := transcript.GetCurrentTodo(data)
	if current != nil {
		subject := current.Subject
		if len(subject) > 30 {
			subject = subject[:27] + "..."
		}
		return colorize("▸", colorYellow, bgYellow, cfg) + " " + subject + " " + colorize(progress, colorGray, bgBlue, cfg)
	}

	// Just show progress
	return colorize("▸", colorYellow, bgYellow, cfg) + " " + colorize(progress, colorGray, bgBlue, cfg)
}

// formatShortDuration formats duration for display (compact)
func formatShortDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins < 60 {
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := mins / 60
	mins = mins % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}
