# Releasing

## Creating a Release

1. Run the release script with the new version:

   ```bash
   ./scripts/release.sh v0.6.0
   ```

   This will:
   - Update version in `.claude-plugin/plugin.json`
   - Update version in `.claude-plugin/marketplace.json`
   - Commit the changes
   - Create a git tag

2. Push the commit and tag:

   ```bash
   git push origin main && git push origin v0.6.0
   ```

3. The GitHub Actions release workflow will automatically:
   - Verify the plugin version matches the tag
   - Build binaries for all platforms using GoReleaser
   - Create a GitHub release with the binaries

## Version Check

The release workflow will fail if the version in `plugin.json` doesn't match the tag. This prevents accidentally releasing with outdated version metadata.

If the check fails, run the release script and force-update the tag:

```bash
./scripts/release.sh v0.6.0
git push origin main
git push origin v0.6.0 --force
```
