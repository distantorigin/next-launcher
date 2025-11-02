# Miriani-Next Updater

Automatic manifest generation for the Miriani-Next updater.

## Overview

The updater (`updater.exe`) can generate the `.manifest` file automatically. This manifest lists all files in your repository with SHA256 hashes and GitHub raw URLs.

## Usage

### Generate Manifest Locally

```bash
cd C:/Users/Tristan/src/miriani-next
C:/Users/Tristan/src/next-updater/updater.exe --generate-manifest
```

This creates `.manifest` in the current directory.

### GitHub Action (Automatic)

The `.github/workflows/auto-version.yml` workflow automatically regenerates the manifest on every push to `main` or `dev` branches.

**What it does:**
1. Checks out your code
2. Builds the updater
3. Runs `updater --generate-manifest`
4. Commits `.manifest` if changed

### Excluded Files

The following are automatically excluded from the manifest:
- `.git`, `.github`, `.gitignore`
- `.manifest`, `.current_version`
- `updater.exe`, `updater`
- `scripts/`
- `README.md`, `LICENSE`
- IDE files (`.vscode`, `.idea`)
- System files (`.DS_Store`, `Thumbs.db`)

## Version Management

**Version is managed manually** - edit `.current_version` by hand:

```json
{
  "major": 1,
  "minor": 0,
  "patch": 0,
  "commit": "abc123def456789",
  "date": "2025-01-22"
}
```

When you're ready to release:

```bash
cd C:/Users/Trisan/src/miriani-next

# Edit .current_version manually
# Then generate manifest
updater.exe --generate-manifest

# Commit
git add .current_version .manifest
git commit -m "chore: release v1.0.0"
git push

# Create release/tag
git tag v1.0.0
git push origin v1.0.0
```

## Setup

1. **Copy workflow to miriani-next:**
   ```bash
   cp -r .github/ C:/Users/Tristan/src/miriani-next/
   ```

2. **Generate initial manifest:**
   ```bash
   cd C:/Users/Tristan/src/miriani-next
   C:/Users/Tristan/src/next-updater/updater.exe --generate-manifest
   ```

3. **Commit and push:**
   ```bash
   git add .manifest .github/
   git commit -m "chore: add auto-manifest generation"
   git push
   ```

That's it! The manifest will auto-regenerate on every push.
