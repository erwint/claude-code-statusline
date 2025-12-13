package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/erwint/claude-code-statusline/internal/config"
)

const (
	githubRepo     = "erwint/claude-code-statusline"
	releasesURL    = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
	downloadURLFmt = "https://github.com/" + githubRepo + "/releases/download/%s/claude-code-statusline_%s_%s.tar.gz"
	updateCheckTTL = 24 * time.Hour
)

type UpdateCache struct {
	LastCheck   time.Time `json:"last_check"`
	LatestVersion string  `json:"latest_version"`
}

type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
}

// CheckForUpdate checks if a newer version is available
func CheckForUpdate(currentVersion string) (*Release, bool, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(releasesURL)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, false, fmt.Errorf("failed to parse release info: %w", err)
	}

	// Compare versions (strip 'v' prefix if present)
	currentVer := strings.TrimPrefix(currentVersion, "v")
	latestVer := strings.TrimPrefix(release.TagName, "v")

	if latestVer == currentVer || latestVer == "" {
		return &release, false, nil
	}

	return &release, true, nil
}

// Update downloads and installs the latest version
func Update(currentVersion string, release *Release) error {
	// Determine platform and architecture
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Construct download URL
	// Format: claude-code-statusline_darwin_arm64.tar.gz
	downloadURL := fmt.Sprintf(downloadURLFmt, release.TagName, goos, goarch)

	config.DebugLog("Downloading from: %s", downloadURL)

	// Download the tar.gz file
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Create temporary file for the new binary
	tmpFile := execPath + ".tmp"

	// Extract binary from tar.gz
	if err := extractBinary(resp.Body, tmpFile); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Create backup
	backupFile := execPath + ".backup"
	os.Remove(backupFile) // Remove old backup if exists
	if err := os.Rename(execPath, backupFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to backup current version: %w", err)
	}

	// Replace with new version
	if err := os.Rename(tmpFile, execPath); err != nil {
		// Try to restore backup
		os.Rename(backupFile, execPath)
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Remove backup on success
	os.Remove(backupFile)

	return nil
}

// extractBinary extracts the claude-code-statusline binary from a tar.gz archive
func extractBinary(r io.Reader, destPath string) error {
	// Create gzip reader
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	// Create tar reader
	tr := tar.NewReader(gzr)

	// Find and extract the binary
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for the claude-code-statusline binary
		if strings.Contains(header.Name, "claude-code-statusline") && !strings.Contains(header.Name, ".") {
			// Found the binary, extract it
			out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer out.Close()

			_, err = io.Copy(out, tr)
			return err
		}
	}

	return fmt.Errorf("binary not found in archive")
}

// CheckForUpdateDaily checks for updates once per day and auto-updates if available
func CheckForUpdateDaily(currentVersion string) {
	cacheFile := getCacheFile()
	cache := loadUpdateCache(cacheFile)

	// Add jitter (±2 hours) to avoid thundering herd
	jitter := time.Duration(rand.Int63n(int64(4*time.Hour))) - 2*time.Hour
	checkInterval := updateCheckTTL + jitter

	// Check if we've checked recently (within 24h ± jitter)
	if time.Since(cache.LastCheck) < checkInterval {
		return
	}

	// Update last check time
	cache.LastCheck = time.Now()

	// Check for updates
	release, hasUpdate, err := CheckForUpdate(currentVersion)
	if err != nil {
		config.DebugLog("Update check failed: %v", err)
		saveUpdateCache(cacheFile, cache)
		return
	}

	if !hasUpdate {
		cache.LatestVersion = currentVersion
		saveUpdateCache(cacheFile, cache)
		return
	}

	// New version available
	cache.LatestVersion = release.TagName
	saveUpdateCache(cacheFile, cache)

	config.DebugLog("New version available: %s (current: %s)", release.TagName, currentVersion)

	// Auto-update in background
	go func() {
		if err := Update(currentVersion, release); err != nil {
			config.DebugLog("Auto-update failed: %v", err)
		} else {
			config.DebugLog("Auto-updated to %s", release.TagName)
		}
	}()
}

func getCacheFile() string {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "claude-code-statusline")
	os.MkdirAll(cacheDir, 0755)
	return filepath.Join(cacheDir, "update_cache.json")
}

func loadUpdateCache(file string) *UpdateCache {
	cache := &UpdateCache{}

	data, err := os.ReadFile(file)
	if err != nil {
		return cache
	}

	json.Unmarshal(data, cache)
	return cache
}

func saveUpdateCache(file string, cache *UpdateCache) {
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	os.WriteFile(file, data, 0644)
}
