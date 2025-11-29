#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="${INSTALL_DIR:-$HOME/.claude/bin}"
BINARY_NAME="claude-code-statusline"
REPO="erwint/claude-code-statusline"

# Download pre-built binary from GitHub releases
download_binary() {
    # Detect OS and architecture
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) echo -e "${YELLOW}Unsupported architecture: $ARCH${NC}"; return 1 ;;
    esac

    case "$OS" in
        darwin|linux) ;;
        mingw*|msys*|cygwin*) OS="windows" ;;
        *) echo -e "${YELLOW}Unsupported OS: $OS${NC}"; return 1 ;;
    esac

    PLATFORM="${OS}_${ARCH}"
    echo -e "${GREEN}Detected platform: $PLATFORM${NC}"

    # Get latest release tag
    LATEST=$(curl -sL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)

    if [ -z "$LATEST" ]; then
        echo -e "${YELLOW}No releases found, building from source...${NC}"
        return 1
    fi

    echo -e "${GREEN}Downloading $BINARY_NAME $LATEST...${NC}"

    EXT="tar.gz"
    [ "$OS" = "windows" ] && EXT="zip"

    BINARY_FILE="$BINARY_NAME"
    [ "$OS" = "windows" ] && BINARY_FILE="${BINARY_NAME}.exe"

    URL="https://github.com/$REPO/releases/download/$LATEST/${BINARY_NAME}_${PLATFORM}.${EXT}"
    echo -e "URL: $URL"

    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT

    if curl -fsSL "$URL" -o "$TMPDIR/archive.$EXT"; then
        cd "$TMPDIR"
        if [ "$EXT" = "zip" ]; then
            unzip -q "archive.$EXT"
        else
            tar xzf "archive.$EXT"
        fi

        if [ -f "$BINARY_FILE" ]; then
            mkdir -p "$INSTALL_DIR"
            mv "$BINARY_FILE" "$INSTALL_DIR/"
            chmod +x "$INSTALL_DIR/$BINARY_FILE"
            echo -e "${GREEN}Installed $BINARY_NAME $LATEST to $INSTALL_DIR${NC}"
            return 0
        else
            echo -e "${YELLOW}Binary not found in archive${NC}"
            ls -la
        fi
    else
        echo -e "${YELLOW}Download failed${NC}"
    fi

    return 1
}

# Build from source
build_from_source() {
    echo -e "${GREEN}Building $BINARY_NAME from source...${NC}"

    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: Go is not installed and no pre-built binary available.${NC}"
        echo -e "Install Go from https://go.dev/dl/ or wait for a release."
        exit 1
    fi

    cd "$(dirname "$0")"
    go build -ldflags="-s -w" -o "$BINARY_NAME" .

    if [ ! -f "$BINARY_NAME" ]; then
        echo -e "${RED}Error: Build failed${NC}"
        exit 1
    fi

    mkdir -p "$INSTALL_DIR"
    mv "$BINARY_NAME" "$INSTALL_DIR/"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"

    echo -e "${GREEN}Built and installed to $INSTALL_DIR/$BINARY_NAME${NC}"
}

# Try download first, fall back to source build
if [ "${BUILD_FROM_SOURCE:-}" = "1" ]; then
    build_from_source
elif ! download_binary; then
    build_from_source
fi

# Configure Claude settings
CLAUDE_SETTINGS="$HOME/.claude/settings.json"

echo ""
echo -e "${GREEN}Configuring Claude Code statusline...${NC}"

if [ -f "$CLAUDE_SETTINGS" ]; then
    cp "$CLAUDE_SETTINGS" "$CLAUDE_SETTINGS.backup"
    echo -e "Backed up existing settings to $CLAUDE_SETTINGS.backup"
fi

if [ -f "$CLAUDE_SETTINGS" ]; then
    if grep -q "statusLine" "$CLAUDE_SETTINGS" 2>/dev/null; then
        echo -e "${YELLOW}statusLine already configured in settings.json${NC}"
    else
        if command -v jq &> /dev/null; then
            jq --arg cmd "$INSTALL_DIR/$BINARY_NAME" \
               '. + {"statusLine": {"type": "command", "command": $cmd}}' \
               "$CLAUDE_SETTINGS" > "$CLAUDE_SETTINGS.tmp" && \
               mv "$CLAUDE_SETTINGS.tmp" "$CLAUDE_SETTINGS"
            echo -e "${GREEN}Added statusLine configuration to settings.json${NC}"
        else
            echo -e "${YELLOW}jq not found. Please manually add to $CLAUDE_SETTINGS:${NC}"
            echo -e '  "statusLine": {"type": "command", "command": "'$INSTALL_DIR/$BINARY_NAME'"}'
        fi
    fi
else
    cat > "$CLAUDE_SETTINGS" << EOF
{
  "statusLine": {
    "type": "command",
    "command": "$INSTALL_DIR/$BINARY_NAME"
  }
}
EOF
    echo -e "${GREEN}Created $CLAUDE_SETTINGS with statusLine configuration${NC}"
fi

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "To test: $INSTALL_DIR/$BINARY_NAME --version"
