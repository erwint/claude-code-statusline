#!/bin/bash
# Usage: ./scripts/release.sh v0.6.0

set -e

VERSION="$1"

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.6.0"
    exit 1
fi

# Strip 'v' prefix for JSON files
VERSION_NUM="${VERSION#v}"

# Update plugin.json
sed -i '' "s/\"version\": \"[^\"]*\"/\"version\": \"$VERSION_NUM\"/" .claude-plugin/plugin.json

# Update marketplace.json
sed -i '' "s/\"version\": \"[^\"]*\"/\"version\": \"$VERSION_NUM\"/" .claude-plugin/marketplace.json

# Commit and tag
git add .claude-plugin/plugin.json .claude-plugin/marketplace.json
git commit -m "Bump version to $VERSION"
git tag "$VERSION"

echo "Version bumped to $VERSION"
echo "Run 'git push origin main && git push origin $VERSION' to release"
