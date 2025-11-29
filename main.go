package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/cost"
	"github.com/erwint/claude-code-statusline/internal/git"
	"github.com/erwint/claude-code-statusline/internal/output"
	"github.com/erwint/claude-code-statusline/internal/session"
	"github.com/erwint/claude-code-statusline/internal/usage"
)

// Set by goreleaser ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

//go:embed pricing.json
var embeddedPricing []byte

func main() {
	// Handle --version before parsing other flags
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" || arg == "-v" {
			fmt.Printf("claude-code-statusline %s (%s) built %s\n", version, commit, date)
			os.Exit(0)
		}
	}

	config.Parse()
	cost.SetEmbeddedPricing(embeddedPricing)

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
