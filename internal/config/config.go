package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	CacheTTL        int
	NoColor         bool
	DisplayMode     string
	InfoMode        string
	Debug           bool
	AggregationMode string // "sliding" or "fixed"
	AutoUpdate      bool
	RequirePlugin   string // Plugin name that must be installed (empty = no requirement)

	// Feature flags for new components
	ShowContext  bool
	ShowTools    bool
	ShowAgents   bool
	ShowTodos    bool
	ShowDuration bool
}

// Global configuration instance
var cfg *Config

// Get returns the global configuration
func Get() *Config {
	if cfg == nil {
		cfg = &Config{}
	}
	return cfg
}

// Parse parses command line flags and environment variables
func Parse() *Config {
	cfg = &Config{}
	flag.IntVar(&cfg.CacheTTL, "cache-ttl", getEnvInt("CLAUDE_STATUS_CACHE_TTL", 300), "Cache TTL in seconds")
	flag.BoolVar(&cfg.NoColor, "no-color", false, "Disable ANSI colors")
	flag.StringVar(&cfg.DisplayMode, "display-mode", getEnv("CLAUDE_STATUS_DISPLAY_MODE", "colors"), "Display mode: colors|minimal|background")
	flag.StringVar(&cfg.InfoMode, "info-mode", getEnv("CLAUDE_STATUS_INFO_MODE", "none"), "Info mode: none|emoji|text")
	flag.StringVar(&cfg.AggregationMode, "aggregation", getEnv("CLAUDE_STATUS_AGGREGATION", "fixed"), "Cost aggregation: sliding|fixed")
	flag.BoolVar(&cfg.Debug, "debug", getEnvBool("CLAUDE_STATUS_DEBUG", false), "Enable debug output")
	flag.BoolVar(&cfg.AutoUpdate, "auto-update", getEnvBool("CLAUDE_STATUS_AUTO_UPDATE", true), "Enable automatic updates (default: true)")
	flag.StringVar(&cfg.RequirePlugin, "require-plugin", "", "Require plugin to be installed (exits silently if not)")

	// Feature flags for new components (all default to true)
	flag.BoolVar(&cfg.ShowContext, "show-context", getEnvBool("CLAUDE_STATUS_CONTEXT", true), "Show context window usage")
	flag.BoolVar(&cfg.ShowTools, "show-tools", getEnvBool("CLAUDE_STATUS_TOOLS", true), "Show tool activity")
	flag.BoolVar(&cfg.ShowAgents, "show-agents", getEnvBool("CLAUDE_STATUS_AGENTS", true), "Show agent activity")
	flag.BoolVar(&cfg.ShowTodos, "show-todos", getEnvBool("CLAUDE_STATUS_TODOS", true), "Show todo progress")
	flag.BoolVar(&cfg.ShowDuration, "show-duration", getEnvBool("CLAUDE_STATUS_DURATION", true), "Show session duration")
	flag.Parse()
	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1" || val == "yes"
	}
	return defaultVal
}

// DebugLog writes debug output to a log file if debug mode is enabled
func DebugLog(format string, args ...interface{}) {
	if cfg == nil || !cfg.Debug {
		return
	}
	f, err := os.OpenFile("/tmp/claude-statusline.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// CheckRequiredPlugin checks if the required plugin is installed.
// If not installed, it removes the statusLine config and returns false.
// Returns true if no plugin is required or if the plugin is installed.
func CheckRequiredPlugin() bool {
	if cfg == nil || cfg.RequirePlugin == "" {
		return true
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return true // Can't check, assume OK
	}

	// Check installed_plugins.json
	pluginsFile := filepath.Join(homeDir, ".claude", "plugins", "installed_plugins.json")
	data, err := os.ReadFile(pluginsFile)
	if err != nil {
		// File doesn't exist or can't read - plugin system not active, clean up
		DebugLog("Cannot read installed_plugins.json: %v", err)
		removeStatusLineConfig(homeDir)
		return false
	}

	var pluginsData struct {
		Plugins map[string]interface{} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &pluginsData); err != nil {
		DebugLog("Cannot parse installed_plugins.json: %v", err)
		return true // Can't parse, assume OK
	}

	// Check if our plugin is in the installed plugins
	// Match exact name or marketplace@plugin format
	for key := range pluginsData.Plugins {
		if key == cfg.RequirePlugin || key == cfg.RequirePlugin+"@"+cfg.RequirePlugin {
			return true
		}
	}

	// Plugin not installed, clean up
	DebugLog("Plugin %s not found in installed plugins, cleaning up", cfg.RequirePlugin)
	removeStatusLineConfig(homeDir)
	// Print dimmed message to inform user why statusline is empty this session
	fmt.Print("\033[2mstatusline plugin disabled\033[0m")
	return false
}

// removeStatusLineConfig removes the statusLine key from settings.json
func removeStatusLineConfig(homeDir string) {
	settingsFile := filepath.Join(homeDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsFile)
	if err != nil {
		return
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return
	}

	if _, exists := settings["statusLine"]; !exists {
		return // Nothing to remove
	}

	delete(settings, "statusLine")

	newData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(settingsFile, newData, 0644)
	DebugLog("Removed statusLine from settings.json")
}
