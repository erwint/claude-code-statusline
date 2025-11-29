package git

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"

	"github.com/erwint/claude-code-statusline/internal/types"
)

// GetInfo retrieves git repository information
func GetInfo() types.GitInfo {
	info := types.GitInfo{}

	// Check if we're in a git repo
	if _, err := runCommand("rev-parse", "--git-dir"); err != nil {
		return info
	}
	info.IsRepo = true

	// Get branch name
	if branch, err := runCommand("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(branch)
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
