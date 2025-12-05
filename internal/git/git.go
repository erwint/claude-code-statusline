package git

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/erwint/claude-code-statusline/internal/types"
)

// GetInfo retrieves git repository information
func GetInfo() types.GitInfo {
	info := types.GitInfo{}

	// Check if we're in a git repo
	gitDir, err := runCommand("rev-parse", "--git-dir")
	if err != nil {
		return info
	}
	info.IsRepo = true
	gitDir = strings.TrimSpace(gitDir)

	// Get branch name
	if branch, err := runCommand("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(branch)

		// If we're in detached HEAD, check for special states
		if info.Branch == "HEAD" {
			info.Branch = getSpecialState(gitDir)
		}
	}

	// Get status
	if status, err := runCommand("status", "--porcelain"); err == nil {
		lines := strings.Split(status, "\n")
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			if strings.HasPrefix(line, "??") {
				info.HasUntracked = true
			}
			if line[0] != ' ' && line[0] != '?' {
				info.HasStaged = true
			}
			if line[1] != ' ' && line[1] != '?' {
				info.HasModified = true
			}
		}
	}

	// Get ahead/behind
	if counts, err := runCommand("rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		parts := strings.Fields(counts)
		if len(parts) == 2 {
			info.Behind, _ = strconv.Atoi(parts[0])
			info.Ahead, _ = strconv.Atoi(parts[1])
		}
	}

	return info
}

func runCommand(args ...string) (string, error) {
	cmdArgs := append([]string{"--no-optional-locks"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	return out.String(), err
}

// getSpecialState detects special Git states (rebase, merge, etc.)
func getSpecialState(gitDir string) string {
	// Check for rebase
	if fileExists(gitDir + "/rebase-merge/head-name") {
		// Interactive rebase
		if branch, err := readFile(gitDir + "/rebase-merge/head-name"); err == nil {
			branch = strings.TrimSpace(branch)
			branch = strings.TrimPrefix(branch, "refs/heads/")
			step := ""
			if msgnum, err := readFile(gitDir + "/rebase-merge/msgnum"); err == nil {
				if end, err := readFile(gitDir + "/rebase-merge/end"); err == nil {
					step = " " + strings.TrimSpace(msgnum) + "/" + strings.TrimSpace(end)
				}
			}
			return "rebasing " + branch + step
		}
		return "rebasing"
	}
	if fileExists(gitDir + "/rebase-apply") {
		// AM-based rebase
		if fileExists(gitDir + "/rebase-apply/rebasing") {
			if branch, err := readFile(gitDir + "/rebase-apply/head-name"); err == nil {
				branch = strings.TrimSpace(branch)
				branch = strings.TrimPrefix(branch, "refs/heads/")
				return "rebasing " + branch
			}
			return "rebasing"
		}
		// git am
		return "am"
	}

	// Check for merge
	if fileExists(gitDir + "/MERGE_HEAD") {
		return "merging"
	}

	// Check for cherry-pick
	if fileExists(gitDir + "/CHERRY_PICK_HEAD") {
		return "cherry-picking"
	}

	// Check for revert
	if fileExists(gitDir + "/REVERT_HEAD") {
		return "reverting"
	}

	// Check for bisect
	if fileExists(gitDir + "/BISECT_LOG") {
		return "bisecting"
	}

	// Detached HEAD - show short commit hash
	if hash, err := runCommand("rev-parse", "--short", "HEAD"); err == nil {
		return "HEAD@" + strings.TrimSpace(hash)
	}

	return "HEAD"
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readFile reads a file and returns its content
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
