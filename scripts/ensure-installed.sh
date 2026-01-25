#!/bin/bash
# Ensures the statusline binary is installed and configured
# Called by SessionStart hook - installs from the plugin's checked-out version

BINARY="$HOME/.claude/bin/claude-code-statusline"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(dirname "$(dirname "$0")")}"
SETTINGS="$HOME/.claude/settings.json"
PLUGIN_NAME="cc-statusline"

# Check if plugin is enabled (exit if disabled)
if [ -f "$SETTINGS" ]; then
    if ! grep -q '"cc-statusline@cc-statusline": *true' "$SETTINGS"; then
        exit 0
    fi
fi

# Get version from plugin.json
VERSION=$(grep '"version"' "$PLUGIN_ROOT/.claude-plugin/plugin.json" | head -1 | sed 's/.*"version": "\([^"]*\)".*/\1/')

# Check if binary needs install/update
NEED_INSTALL=false
if [ ! -x "$BINARY" ]; then
    NEED_INSTALL=true
else
    # Check installed version
    INSTALLED_VERSION=$("$BINARY" --version 2>/dev/null | head -1 | sed 's/.*statusline \([^ ]*\).*/\1/')
    if [ "$INSTALLED_VERSION" != "$VERSION" ]; then
        NEED_INSTALL=true
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    if [ -n "$INSTALLED_VERSION" ]; then
        echo "Updating claude-code-statusline $INSTALLED_VERSION -> v$VERSION..."
    else
        echo "Installing claude-code-statusline v$VERSION..."
    fi

    # Detect platform
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        darwin) PLATFORM="darwin" ;;
        linux) PLATFORM="linux" ;;
        mingw*|msys*|cygwin*) PLATFORM="windows" ;;
        *) echo "Unsupported OS: $OS"; exit 1 ;;
    esac

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    PLATFORM="${PLATFORM}_${ARCH}"

    # Download and install
    mkdir -p "$HOME/.claude/bin"

    if [ "$PLATFORM" = "windows_amd64" ] || [ "$PLATFORM" = "windows_arm64" ]; then
        URL="https://github.com/erwint/claude-code-statusline/releases/download/v${VERSION}/claude-code-statusline_${PLATFORM}.zip"
        curl -fsSL "$URL" -o /tmp/statusline.zip
        unzip -o /tmp/statusline.zip -d "$HOME/.claude/bin"
        rm /tmp/statusline.zip
    else
        URL="https://github.com/erwint/claude-code-statusline/releases/download/v${VERSION}/claude-code-statusline_${PLATFORM}.tar.gz"
        curl -fsSL "$URL" | tar -xz -C "$HOME/.claude/bin"
        chmod +x "$BINARY"
    fi
fi

# Always ensure settings.json is configured
STATUSLINE_CMD="~/.claude/bin/claude-code-statusline --require-plugin=${PLUGIN_NAME}"

if [ -f "$SETTINGS" ]; then
    if ! grep -q '"statusLine"' "$SETTINGS"; then
        # Add statusLine to existing settings
        tmp=$(mktemp)
        sed "s/^{$/{\n  \"statusLine\": { \"type\": \"command\", \"command\": \"${STATUSLINE_CMD//\//\\/}\" },/" "$SETTINGS" > "$tmp"
        mv "$tmp" "$SETTINGS"
    fi
else
    # Create new settings file
    cat > "$SETTINGS" << EOF
{
  "statusLine": {
    "type": "command",
    "command": "${STATUSLINE_CMD}"
  }
}
EOF
fi
