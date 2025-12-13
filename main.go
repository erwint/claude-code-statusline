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
	"github.com/erwint/claude-code-statusline/internal/updater"
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

func handleUpdate() {
	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for updates...")

	release, hasUpdate, err := updater.CheckForUpdate(version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
		os.Exit(1)
	}

	if !hasUpdate {
		fmt.Println("Already running the latest version!")
		return
	}

	fmt.Printf("New version available: %s\n", release.TagName)
	fmt.Printf("Downloading and installing...\n")

	if err := updater.Update(version, release); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Successfully updated to %s\n", release.TagName)
	fmt.Println("Run the command again to use the new version.")
}

func main() {
	// Handle --version and --update before parsing other flags
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" || arg == "-v" {
			fmt.Printf("claude-code-statusline %s (%s) built %s\n", version, commit, date)
			os.Exit(0)
		}
		if arg == "--update" {
			handleUpdate()
			os.Exit(0)
		}
	}

	cfg := config.Parse()
	cost.SetEmbeddedPricing(embeddedPricing)

	// Check for updates once per day if auto-update is enabled (with jitter to avoid thundering herd)
	if cfg.AutoUpdate {
		go updater.CheckForUpdateDaily(version)
	}

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
