# Building Miriani-Next Updater

## Quick Build (For Distribution)

Simply run the build script:

```batch
build.bat
```

This creates `updater.exe` with all personal paths removed and debug information stripped.

## Manual Build

If you prefer to build manually:

```bash
go build -trimpath -ldflags="-s -w" -o updater.exe
```

### Build Flags Explained

- **`-trimpath`**: Removes file system paths from the binary (replaces with module paths)
  - Prevents leaking your local directory structure
  - Essential for distribution

- **`-ldflags="-s -w"`**: Strips debug information
  - `-s`: Omit symbol table
  - `-w`: Omit DWARF debug info
  - Reduces binary size significantly

## Development Build

For development with full debug info:

```bash
go build -o updater.exe
```

## Verification

To verify no personal paths are embedded:

```bash
# Search for common path patterns
findstr /C:"Users\Tristan" updater.exe
findstr /C:"C:\Users" updater.exe

# No output = success!
```

## Testing Before Distribution

Always test the distribution build before releasing:

```bash
# Build for distribution
build.bat

# Test basic functionality
updater.exe --help
updater.exe --version
updater.exe --check

# Test in a clean directory
mkdir test-install
cd test-install
..\updater.exe
```

## Cross-Platform Builds

To build for other platforms:

```bash
# Windows 64-bit (default)
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o updater.exe

# Windows 32-bit
set GOOS=windows
set GOARCH=386
go build -trimpath -ldflags="-s -w" -o updater-32bit.exe
```
