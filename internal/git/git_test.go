package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetSpecialState(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(gitDir string) error
		expected string
	}{
		{
			name: "interactive rebase with progress",
			setup: func(gitDir string) error {
				rebaseMerge := filepath.Join(gitDir, "rebase-merge")
				if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseMerge, "head-name"), []byte("refs/heads/feature-branch\n"), 0644); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseMerge, "msgnum"), []byte("3\n"), 0644); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseMerge, "end"), []byte("7\n"), 0644); err != nil {
					return err
				}
				return nil
			},
			expected: "rebasing feature-branch 3/7",
		},
		{
			name: "interactive rebase without progress",
			setup: func(gitDir string) error {
				rebaseMerge := filepath.Join(gitDir, "rebase-merge")
				if err := os.MkdirAll(rebaseMerge, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseMerge, "head-name"), []byte("refs/heads/my-branch\n"), 0644); err != nil {
					return err
				}
				return nil
			},
			expected: "rebasing my-branch",
		},
		{
			name: "am-based rebase",
			setup: func(gitDir string) error {
				rebaseApply := filepath.Join(gitDir, "rebase-apply")
				if err := os.MkdirAll(rebaseApply, 0755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseApply, "rebasing"), []byte(""), 0644); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(rebaseApply, "head-name"), []byte("refs/heads/test-branch\n"), 0644); err != nil {
					return err
				}
				return nil
			},
			expected: "rebasing test-branch",
		},
		{
			name: "git am",
			setup: func(gitDir string) error {
				rebaseApply := filepath.Join(gitDir, "rebase-apply")
				if err := os.MkdirAll(rebaseApply, 0755); err != nil {
					return err
				}
				// No "rebasing" file means it's a git am
				return nil
			},
			expected: "am",
		},
		{
			name: "merge in progress",
			setup: func(gitDir string) error {
				return os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte("abc123\n"), 0644)
			},
			expected: "merging",
		},
		{
			name: "cherry-pick in progress",
			setup: func(gitDir string) error {
				return os.WriteFile(filepath.Join(gitDir, "CHERRY_PICK_HEAD"), []byte("abc123\n"), 0644)
			},
			expected: "cherry-picking",
		},
		{
			name: "revert in progress",
			setup: func(gitDir string) error {
				return os.WriteFile(filepath.Join(gitDir, "REVERT_HEAD"), []byte("abc123\n"), 0644)
			},
			expected: "reverting",
		},
		{
			name: "bisect in progress",
			setup: func(gitDir string) error {
				return os.WriteFile(filepath.Join(gitDir, "BISECT_LOG"), []byte("git bisect start\n"), 0644)
			},
			expected: "bisecting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary git directory
			tmpDir, err := os.MkdirTemp("", "git-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Setup the test scenario
			if err := tt.setup(tmpDir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Test the function
			result := getSpecialState(tmpDir)
			if result != tt.expected {
				t.Errorf("getSpecialState() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Test file exists
	if !fileExists(tmpPath) {
		t.Error("fileExists() returned false for existing file")
	}

	// Test file doesn't exist
	if fileExists(tmpPath + ".nonexistent") {
		t.Error("fileExists() returned true for non-existent file")
	}
}

func TestReadFile(t *testing.T) {
	// Create a temporary file with content
	tmpFile, err := os.CreateTemp("", "test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	expected := "test content\n"
	if _, err := tmpFile.WriteString(expected); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Test reading file
	content, err := readFile(tmpPath)
	if err != nil {
		t.Errorf("readFile() error = %v", err)
	}
	if content != expected {
		t.Errorf("readFile() = %q, want %q", content, expected)
	}

	// Test reading non-existent file
	_, err = readFile(tmpPath + ".nonexistent")
	if err == nil {
		t.Error("readFile() should return error for non-existent file")
	}
}
