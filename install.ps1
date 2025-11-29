# PowerShell install script for Windows
$ErrorActionPreference = "Stop"

$INSTALL_DIR = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:USERPROFILE\.claude\bin" }
$BINARY_NAME = "claude-code-statusline"
$REPO = "erwint/claude-code-statusline"

function Download-Binary {
    $ARCH = if ([Environment]::Is64BitOperatingSystem) {
        if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
    } else {
        Write-Host "32-bit systems not supported" -ForegroundColor Red
        return $false
    }

    $PLATFORM = "windows_$ARCH"
    Write-Host "Detected platform: $PLATFORM" -ForegroundColor Green

    # Get latest release
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$REPO/releases/latest" -UseBasicParsing
        $LATEST = $release.tag_name
    } catch {
        Write-Host "No releases found" -ForegroundColor Yellow
        return $false
    }

    Write-Host "Downloading $BINARY_NAME $LATEST..." -ForegroundColor Green

    $URL = "https://github.com/$REPO/releases/download/$LATEST/${BINARY_NAME}_${PLATFORM}.zip"
    Write-Host "URL: $URL"

    $TMPDIR = New-TemporaryFile | ForEach-Object { Remove-Item $_; New-Item -ItemType Directory -Path $_ }

    try {
        $archivePath = Join-Path $TMPDIR "archive.zip"
        Invoke-WebRequest -Uri $URL -OutFile $archivePath -UseBasicParsing

        Expand-Archive -Path $archivePath -DestinationPath $TMPDIR -Force

        $binaryPath = Join-Path $TMPDIR "$BINARY_NAME.exe"
        if (Test-Path $binaryPath) {
            New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
            Move-Item -Path $binaryPath -Destination $INSTALL_DIR -Force
            Write-Host "Installed $BINARY_NAME $LATEST to $INSTALL_DIR" -ForegroundColor Green
            return $true
        } else {
            Write-Host "Binary not found in archive" -ForegroundColor Yellow
            return $false
        }
    } catch {
        Write-Host "Download failed: $_" -ForegroundColor Yellow
        return $false
    } finally {
        Remove-Item -Path $TMPDIR -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Download binary
if (-not (Download-Binary)) {
    Write-Host "Failed to download binary. Please build from source or check releases." -ForegroundColor Red
    Write-Host "  go build -ldflags='-s -w' -o $BINARY_NAME.exe ."
    exit 1
}

# Configure Claude settings
$CLAUDE_SETTINGS = "$env:USERPROFILE\.claude\settings.json"
$binaryFullPath = Join-Path $INSTALL_DIR "$BINARY_NAME.exe"

Write-Host ""
Write-Host "Configuring Claude Code statusline..." -ForegroundColor Green

if (Test-Path $CLAUDE_SETTINGS) {
    Copy-Item $CLAUDE_SETTINGS "$CLAUDE_SETTINGS.backup"
    Write-Host "Backed up existing settings to $CLAUDE_SETTINGS.backup"

    $settings = Get-Content $CLAUDE_SETTINGS -Raw | ConvertFrom-Json
    if ($settings.statusLine) {
        Write-Host "statusLine already configured in settings.json" -ForegroundColor Yellow
    } else {
        $settings | Add-Member -NotePropertyName "statusLine" -NotePropertyValue @{
            type = "command"
            command = $binaryFullPath
        }
        $settings | ConvertTo-Json -Depth 10 | Set-Content $CLAUDE_SETTINGS
        Write-Host "Added statusLine configuration to settings.json" -ForegroundColor Green
    }
} else {
    New-Item -ItemType Directory -Path (Split-Path $CLAUDE_SETTINGS) -Force | Out-Null
    @{
        statusLine = @{
            type = "command"
            command = $binaryFullPath
        }
    } | ConvertTo-Json -Depth 10 | Set-Content $CLAUDE_SETTINGS
    Write-Host "Created $CLAUDE_SETTINGS with statusLine configuration" -ForegroundColor Green
}

Write-Host ""
Write-Host "Installation complete!" -ForegroundColor Green
Write-Host ""
Write-Host "To test: & '$binaryFullPath' --version"
