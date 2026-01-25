---
description: Install or troubleshoot claude-code-statusline
---

# Setup claude-code-statusline

Install the statusline binary and configure Claude Code to use it.

**Note:** The plugin normally installs automatically on session start. Use this command if the statusline doesn't appear or for troubleshooting.

## Quick Diagnosis

First, check what's wrong:

1. **Check if binary exists:**
   ```bash
   ls -la ~/.claude/bin/claude-code-statusline
   ```

2. **Check if binary works:**
   ```bash
   ~/.claude/bin/claude-code-statusline --version
   ```

3. **Check settings.json has statusLine config:**
   ```bash
   grep statusLine ~/.claude/settings.json
   ```

If any of these fail, proceed with the steps below.

## Steps

### 1. Detect platform and architecture

Determine the platform from your environment context (don't rely solely on `uname` which can be wrong in Git Bash on Windows).

**Platforms:**
- macOS ARM64 (Apple Silicon): `darwin_arm64`
- macOS Intel: `darwin_amd64`
- Linux x64: `linux_amd64`
- Linux ARM64: `linux_arm64`
- Windows x64: `windows_amd64`
- Windows ARM64: `windows_arm64`

### 2. Download and install binary

**macOS / Linux:**

```bash
# Get latest version tag
LATEST=$(curl -sL "https://api.github.com/repos/erwint/claude-code-statusline/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)

# Set platform (detect with: uname -s and uname -m)
PLATFORM="darwin_arm64"  # adjust based on detection

# Download and extract
mkdir -p ~/.claude/bin
curl -fsSL "https://github.com/erwint/claude-code-statusline/releases/download/${LATEST}/claude-code-statusline_${PLATFORM}.tar.gz" | tar -xz -C ~/.claude/bin

# Make executable
chmod +x ~/.claude/bin/claude-code-statusline
```

**Windows (PowerShell):**

```powershell
# Get latest version
$release = Invoke-RestMethod -Uri "https://api.github.com/repos/erwint/claude-code-statusline/releases/latest"
$LATEST = $release.tag_name

# Set platform
$PLATFORM = "windows_amd64"  # or windows_arm64

# Download and extract
$binDir = "$env:USERPROFILE\.claude\bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$zipUrl = "https://github.com/erwint/claude-code-statusline/releases/download/$LATEST/claude-code-statusline_${PLATFORM}.zip"
$zipPath = "$env:TEMP\claude-code-statusline.zip"
Invoke-WebRequest -Uri $zipUrl -OutFile $zipPath
Expand-Archive -Path $zipPath -DestinationPath $binDir -Force
Remove-Item $zipPath
```

### 3. Configure statusLine

Read `~/.claude/settings.json` (or create if it doesn't exist) and add/merge the `statusLine` configuration:

**macOS / Linux:**
```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.claude/bin/claude-code-statusline"
  }
}
```

**Windows:**
```json
{
  "statusLine": {
    "type": "command",
    "command": "%USERPROFILE%\\.claude\\bin\\claude-code-statusline.exe"
  }
}
```

If the file already has other settings, preserve them and only add/update the `statusLine` key.

### 4. Verify installation

Test that the binary works:

**macOS / Linux:**
```bash
~/.claude/bin/claude-code-statusline --version
```

**Windows:**
```powershell
& "$env:USERPROFILE\.claude\bin\claude-code-statusline.exe" --version
```

### 5. Complete

Tell the user:
- Installation successful
- Restart Claude Code to see the new statusline
- Link to documentation: https://github.com/erwint/claude-code-statusline

## Troubleshooting

### Binary fails to download
- Check internet connectivity
- Verify the platform detection was correct
- Try downloading manually from https://github.com/erwint/claude-code-statusline/releases

### Statusline doesn't appear after restart
- Verify `~/.claude/settings.json` has the `statusLine` configuration (see step 3)
- Check that the binary exists at `~/.claude/bin/claude-code-statusline`
- Run the binary manually to check for errors:
  ```bash
  echo '{}' | ~/.claude/bin/claude-code-statusline
  ```

### Binary exists but statusline is blank
- Check if you're logged in with a Claude subscription (required for API usage data)
- Try with debug mode to see what's happening:
  ```bash
  echo '{}' | CLAUDE_STATUS_DEBUG=true ~/.claude/bin/claude-code-statusline
  ```
- Check debug log at `/tmp/claude-statusline.log`

### Wrong version installed
- Force reinstall by removing the binary and restarting:
  ```bash
  rm ~/.claude/bin/claude-code-statusline
  ```
- Or update manually:
  ```bash
  ~/.claude/bin/claude-code-statusline --update
  ```

## Notes

- The statusline displays: git status, model, context bar, subscription, costs, API usage, tools, agents, todos, and session duration
- Configure features via environment variables (e.g., `CLAUDE_STATUS_CONTEXT=false` to disable context bar)
- The binary auto-updates daily by default
- Documentation: https://github.com/erwint/claude-code-statusline
