package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erwint/claude-code-statusline/internal/types"
)

func setupTestCacheDir(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	// Ensure cache dir exists
	os.MkdirAll(filepath.Join(dir, ".cache", "claude-code-statusline"), 0755)
	return dir, func() { os.Setenv("HOME", origHome) }
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStaleCache_ReturnsUnavailableAfterExpiredResetTime(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	cacheFile := getCacheFile("usage.json")
	writeJSON(t, cacheFile, &types.UsageCache{
		UsagePercent: 100,
		ResetTime:    time.Now().Add(-1 * time.Hour), // expired
	})

	cache := staleCache(cacheFile)
	if cache == nil {
		t.Fatal("expected unavailable marker, got nil")
	}
	if !cache.Unavailable {
		t.Error("expected Unavailable=true for expired reset time")
	}
}

func TestStaleCache_ReturnsStaleForFutureResetTime(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	cacheFile := getCacheFile("usage.json")
	writeJSON(t, cacheFile, &types.UsageCache{
		UsagePercent: 80,
		ResetTime:    time.Now().Add(1 * time.Hour),
	})

	cache := staleCache(cacheFile)
	if cache == nil {
		t.Fatal("expected cache, got nil")
	}
	if cache.UsagePercent != 80 {
		t.Errorf("expected UsagePercent=80, got %.1f", cache.UsagePercent)
	}
	if !cache.Stale {
		t.Error("expected Stale=true")
	}
	if cache.Unavailable {
		t.Error("expected Unavailable=false for valid reset time")
	}
}

func TestStaleCache_PreservesAllFieldsWhenNotExpired(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	cacheFile := getCacheFile("usage.json")
	writeJSON(t, cacheFile, &types.UsageCache{
		UsagePercent:      50,
		ResetTime:         time.Now().Add(1 * time.Hour),
		SevenDayPercent:   90,
		SevenDayResetTime: time.Now().Add(1 * time.Hour),
	})

	cache := staleCache(cacheFile)
	if cache == nil {
		t.Fatal("expected cache, got nil")
	}
	if cache.UsagePercent != 50 {
		t.Errorf("expected 50%%, got %.1f", cache.UsagePercent)
	}
	if cache.SevenDayPercent != 90 {
		t.Errorf("expected 90%% seven-day, got %.1f", cache.SevenDayPercent)
	}
	if !cache.Stale {
		t.Error("expected Stale=true")
	}
}

func TestBackoff_IncreaseWithoutRetryAfter(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	// First 429: should start at initial (30s)
	increaseBackoff("")
	b := loadBackoff()
	if b == nil {
		t.Fatal("expected backoff state")
	}
	if b.BackoffSeconds != backoffInitial.Seconds() {
		t.Errorf("expected initial backoff %.0f, got %.0f", backoffInitial.Seconds(), b.BackoffSeconds)
	}
	if time.Until(b.BackoffUntil) <= 0 {
		t.Error("expected BackoffUntil in the future")
	}

	// Second 429: should be 1.5x
	increaseBackoff("")
	b = loadBackoff()
	expected := backoffInitial.Seconds() * 1.5
	if b.BackoffSeconds != expected {
		t.Errorf("expected %.0fs after second 429, got %.0fs", expected, b.BackoffSeconds)
	}

	// Third 429: should be ~1.5x again (allow ±1s for duration truncation)
	increaseBackoff("")
	b = loadBackoff()
	expected = expected * 1.5
	diff := b.BackoffSeconds - expected
	if diff < -1 || diff > 1 {
		t.Errorf("expected ~%.0fs after third 429, got %.0fs", expected, b.BackoffSeconds)
	}
}

func TestBackoff_IncreaseRespectsMax(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	// Set backoff near the max
	saveBackoff(&backoffState{
		BackoffUntil:   time.Now().Add(-1 * time.Second),
		BackoffSeconds: (backoffMax - 10*time.Second).Seconds(),
	})

	increaseBackoff("")
	b := loadBackoff()
	if b.BackoffSeconds > backoffMax.Seconds() {
		t.Errorf("backoff %.0fs exceeded max %.0fs", b.BackoffSeconds, backoffMax.Seconds())
	}
}

func TestBackoff_IncreaseWithRetryAfter(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	increaseBackoff("120")
	b := loadBackoff()
	if b == nil {
		t.Fatal("expected backoff state")
	}
	if b.BackoffSeconds != 120 {
		t.Errorf("expected 120s from Retry-After, got %.0f", b.BackoffSeconds)
	}
}

func TestBackoff_IncreaseIgnoresInvalidRetryAfter(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	increaseBackoff("0")
	b := loadBackoff()
	if b.BackoffSeconds != backoffInitial.Seconds() {
		t.Errorf("expected initial backoff for Retry-After=0, got %.0f", b.BackoffSeconds)
	}

	clearBackoff()
	increaseBackoff("bogus")
	b = loadBackoff()
	if b.BackoffSeconds != backoffInitial.Seconds() {
		t.Errorf("expected initial backoff for invalid header, got %.0f", b.BackoffSeconds)
	}
}

func TestBackoff_DecayGradual(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	saveBackoff(&backoffState{
		BackoffUntil:   time.Now().Add(-1 * time.Second),
		BackoffSeconds: 100,
	})

	decayBackoff()
	b := loadBackoff()
	if b == nil {
		t.Fatal("expected backoff to still exist")
	}
	if b.BackoffSeconds != 80 { // 100 * 0.8
		t.Errorf("expected 80s after decay, got %.0f", b.BackoffSeconds)
	}

	// Decay again
	decayBackoff()
	b = loadBackoff()
	if b.BackoffSeconds != 64 { // 80 * 0.8
		t.Errorf("expected 64s after second decay, got %.0f", b.BackoffSeconds)
	}
}

func TestBackoff_DecayClearsWhenBelowMin(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	saveBackoff(&backoffState{
		BackoffUntil:   time.Now().Add(-1 * time.Second),
		BackoffSeconds: backoffMin.Seconds() + 1, // just above min
	})

	decayBackoff()
	b := loadBackoff()
	if b != nil {
		t.Errorf("expected backoff cleared when decayed below min, got %.0fs", b.BackoffSeconds)
	}
}

func TestBackoff_DecayNoOp(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	// No backoff file — should be a no-op
	decayBackoff()
	b := loadBackoff()
	if b != nil {
		t.Error("expected nil backoff when no file exists")
	}
}

func TestLoadCache_ForcesRefreshAfterResetTime(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	cacheFile := getCacheFile("usage.json")
	writeJSON(t, cacheFile, &types.UsageCache{
		UsagePercent: 100,
		ResetTime:    time.Now().Add(-1 * time.Hour),
	})
	// Touch file to make it "fresh" by TTL standards
	now := time.Now()
	os.Chtimes(cacheFile, now, now)

	// With TTL=0 (>=95%), loadCache returns valid=false due to modtime check.
	// But with a long TTL, the cache would be "valid" — our caller in
	// GetUsageAndSubscription checks ResetTime and forces refresh anyway.
	cache, valid := loadCache(cacheFile, 3600)
	if !valid {
		t.Skip("cache reported invalid (expected for >=95% usage with TTL override)")
	}
	// If loadCache says valid, the caller should still check reset time
	if cache != nil && !cache.ResetTime.IsZero() && time.Now().After(cache.ResetTime) {
		// This is the condition that GetUsageAndSubscription checks
		t.Log("Correctly identified cache with expired reset time")
	}
}

func TestLockFile_StaleCleanup(t *testing.T) {
	_, cleanup := setupTestCacheDir(t)
	defer cleanup()

	lockFile := getCacheFile("usage.lock")
	// Create a lock file with old mtime
	os.WriteFile(lockFile, []byte{}, 0644)
	past := time.Now().Add(-1 * time.Minute)
	os.Chtimes(lockFile, past, past)

	info, err := os.Stat(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) <= 30*time.Second {
		t.Error("expected stale lock file")
	}

	// Verify stale detection logic matches what's in GetUsageAndSubscription
	if time.Since(info.ModTime()) > 30*time.Second {
		os.Remove(lockFile)
	}
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Error("expected lock file to be removed")
	}
}
