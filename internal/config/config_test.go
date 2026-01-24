package config

import (
	"os"
	"testing"
)

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		defVal   bool
		expected bool
	}{
		{"true string", "true", false, true},
		{"1 string", "1", false, true},
		{"yes string", "yes", false, true},
		{"false string", "false", true, false},
		{"0 string", "0", true, false},
		{"no string", "no", true, false},
		{"empty with default false", "", false, false},
		{"empty with default true", "", true, true},
		{"invalid uses default", "invalid", true, false}, // doesn't match true values
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_BOOL_" + tt.name
			if tt.value != "" {
				os.Setenv(key, tt.value)
				defer os.Unsetenv(key)
			}

			result := getEnvBool(key, tt.defVal)
			if result != tt.expected {
				t.Errorf("getEnvBool(%q, %v) with value %q = %v, want %v",
					key, tt.defVal, tt.value, result, tt.expected)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		defVal   int
		expected int
	}{
		{"valid int", "100", 300, 100},
		{"zero", "0", 300, 0},
		{"invalid uses default", "invalid", 300, 300},
		{"empty uses default", "", 300, 300},
		{"negative", "-50", 300, -50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_INT_" + tt.name
			if tt.value != "" {
				os.Setenv(key, tt.value)
				defer os.Unsetenv(key)
			}

			result := getEnvInt(key, tt.defVal)
			if result != tt.expected {
				t.Errorf("getEnvInt(%q, %d) with value %q = %d, want %d",
					key, tt.defVal, tt.value, result, tt.expected)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		defVal   string
		expected string
	}{
		{"value set", "custom", "default", "custom"},
		{"empty uses default", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_ENV_" + tt.name
			if tt.value != "" {
				os.Setenv(key, tt.value)
				defer os.Unsetenv(key)
			}

			result := getEnv(key, tt.defVal)
			if result != tt.expected {
				t.Errorf("getEnv(%q, %q) with value %q = %q, want %q",
					key, tt.defVal, tt.value, result, tt.expected)
			}
		})
	}
}

func TestConfigGet(t *testing.T) {
	// Test that Get() returns a non-nil config
	cfg = nil // Reset
	config := Get()
	if config == nil {
		t.Error("Get() should never return nil")
	}
}

func TestConfigFeatureFlagDefaults(t *testing.T) {
	// Test that when env vars are not set, the default is true for feature flags
	// This tests the getEnvBool behavior with default true

	// Ensure env vars are not set
	os.Unsetenv("CLAUDE_STATUS_CONTEXT")
	os.Unsetenv("CLAUDE_STATUS_TOOLS")
	os.Unsetenv("CLAUDE_STATUS_AGENTS")
	os.Unsetenv("CLAUDE_STATUS_TODOS")
	os.Unsetenv("CLAUDE_STATUS_DURATION")

	// Verify defaults
	if !getEnvBool("CLAUDE_STATUS_CONTEXT", true) {
		t.Error("CLAUDE_STATUS_CONTEXT default should be true")
	}
	if !getEnvBool("CLAUDE_STATUS_TOOLS", true) {
		t.Error("CLAUDE_STATUS_TOOLS default should be true")
	}
	if !getEnvBool("CLAUDE_STATUS_AGENTS", true) {
		t.Error("CLAUDE_STATUS_AGENTS default should be true")
	}
	if !getEnvBool("CLAUDE_STATUS_TODOS", true) {
		t.Error("CLAUDE_STATUS_TODOS default should be true")
	}
	if !getEnvBool("CLAUDE_STATUS_DURATION", true) {
		t.Error("CLAUDE_STATUS_DURATION default should be true")
	}
}

func TestConfigFeatureFlagOverrides(t *testing.T) {
	// Test that env vars can disable features
	os.Setenv("CLAUDE_STATUS_CONTEXT", "false")
	os.Setenv("CLAUDE_STATUS_TOOLS", "0")
	defer os.Unsetenv("CLAUDE_STATUS_CONTEXT")
	defer os.Unsetenv("CLAUDE_STATUS_TOOLS")

	if getEnvBool("CLAUDE_STATUS_CONTEXT", true) {
		t.Error("CLAUDE_STATUS_CONTEXT should be false when set to 'false'")
	}
	if getEnvBool("CLAUDE_STATUS_TOOLS", true) {
		t.Error("CLAUDE_STATUS_TOOLS should be false when set to '0'")
	}
}
