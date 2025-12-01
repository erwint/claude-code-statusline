package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	CacheTTL       int
	NoColor        bool
	DisplayMode    string
	InfoMode       string
	Debug          bool
	AggregationMode string // "sliding" or "fixed"
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
	flag.IntVar(&cfg.CacheTTL, "cache-ttl", getEnvInt("CLAUDE_STATUSLINE_CACHE_TTL", 300), "Cache TTL in seconds")
	flag.BoolVar(&cfg.NoColor, "no-color", false, "Disable ANSI colors")
	flag.StringVar(&cfg.DisplayMode, "display-mode", getEnv("CLAUDE_STATUS_DISPLAY_MODE", "colors"), "Display mode: colors|minimal|background")
	flag.StringVar(&cfg.InfoMode, "info-mode", getEnv("CLAUDE_STATUS_INFO_MODE", "none"), "Info mode: none|emoji|text")
	flag.StringVar(&cfg.AggregationMode, "aggregation", getEnv("CLAUDE_STATUS_AGGREGATION", "fixed"), "Cost aggregation: sliding|fixed")
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug output")
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
