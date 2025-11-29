package main

import (
	"fmt"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/cost"
	"github.com/erwint/claude-code-statusline/internal/git"
	"github.com/erwint/claude-code-statusline/internal/output"
	"github.com/erwint/claude-code-statusline/internal/session"
	"github.com/erwint/claude-code-statusline/internal/usage"
)

func main() {
	config.Parse()

	// Read session input from stdin (if available)
	sess := session.ReadInput()

	// Get all the status components
	gitInfo := git.GetInfo()
	usageData, subscription, tier := usage.GetUsageAndSubscription()
	tokenStats := cost.GetTokenStats()

	// Format and output
	out := output.FormatStatusLine(sess, gitInfo, usageData, tokenStats, subscription, tier)
	fmt.Print(out)
}
