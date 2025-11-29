#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

INSTALL_DIR="${INSTALL_DIR:-$HOME/.claude/bin}"
BINARY_NAME="claude-code-statusline"

echo -e "${GREEN}Building claude-code-statusline...${NC}"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed. Please install Go first.${NC}"
    exit 1
fi

# Build the binary
cd "$(dirname "$0")"
go build -o "$BINARY_NAME" .

if [ ! -f "$BINARY_NAME" ]; then
    echo -e "${RED}Error: Build failed${NC}"
    exit 1
fi

echo -e "${GREEN}Build successful!${NC}"

# Create install directory if it doesn't exist
mkdir -p "$INSTALL_DIR"

# Copy binary to install directory
cp "$BINARY_NAME" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$BINARY_NAME"
rm "$BINARY_NAME"

echo -e "${GREEN}Installed to $INSTALL_DIR/$BINARY_NAME${NC}"

# No need to check PATH - Claude Code uses absolute path in settings

# Configure Claude settings
CLAUDE_SETTINGS="$HOME/.claude/settings.json"

echo ""
echo -e "${GREEN}Configuring Claude Code statusline...${NC}"

if [ -f "$CLAUDE_SETTINGS" ]; then
    # Backup existing settings
    cp "$CLAUDE_SETTINGS" "$CLAUDE_SETTINGS.backup"
    echo -e "Backed up existing settings to $CLAUDE_SETTINGS.backup"
fi

# Create or update settings.json
# We need to add the status line configuration
if [ -f "$CLAUDE_SETTINGS" ]; then
    # Check if settings already has statusline config
    if grep -q "statusLine" "$CLAUDE_SETTINGS" 2>/dev/null; then
        echo -e "${YELLOW}statusLine already configured in settings.json${NC}"
        echo -e "Current configuration preserved. To update manually, edit:"
        echo -e "  $CLAUDE_SETTINGS"
    else
        # Add statusline config to existing settings
        # This is a simple approach - for complex JSON manipulation, use jq
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
    # Create new settings file
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
echo "Usage:"
echo "  The statusline will automatically appear in Claude Code."
echo ""
echo "Configuration (optional environment variables):"
echo "  CLAUDE_STATUSLINE_CACHE_TTL  - Cache TTL in seconds (default: 300)"
echo "  CLAUDE_STATUS_DISPLAY_MODE   - colors|minimal|background (default: colors)"
echo "  CLAUDE_STATUS_INFO_MODE      - none|emoji|text (default: none)"
echo ""
echo "To test manually:"
echo "  $INSTALL_DIR/$BINARY_NAME"
