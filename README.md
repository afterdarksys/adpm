ADPM - AfterDark Package Manager
=================================

**Homage to Todd Bennett III, unixeng**

A lightweight package manager for bundling complex dependencies (especially C libraries) with Python projects.

## The Problem

Python packages with C dependencies are a pain:

```bash
pip install icloud-cli[full]
# Error: libimobiledevice not found
# Solution: brew install libimobiledevice  # macOS
# Solution: apt install libimobiledevice6  # Linux
# Solution: ??? # Windows... good luck
```

Different package managers, different paths, version conflicts, compilation failures...

## The ADPM Solution

Bundle pre-compiled binaries in a platform-aware archive:

```bash
./icloud-cli-installer
# ✓ Auto-detects your platform
# ✓ Installs appropriate binaries
# ✓ No external dependencies
# ✓ Just works™
```

## Package Format

`.adpm` files are `cpio.bz2` archives containing:

```
package.adpm (cpio.bz2)
├── META.json           # Package metadata
├── INSTALL.sh          # Installation logic
├── bin/                # Platform-specific binaries
│   ├── darwin-arm64/
│   ├── darwin-x86_64/
│   ├── linux-x86_64/
│   └── linux-aarch64/
├── lib/                # Platform-specific libraries
│   └── [same structure]
└── python/             # Python wheels
    └── *.whl
```

### Why cpio.bz2?

- **Standard Unix format** - Available everywhere
- **Better than tar** for special files
- **Good compression** - bzip2 is widely available
- **Easy scripting** - Standard tools

## Quick Start

### Building a Package

```bash
# Build package with libimobiledevice binaries
./adpm/builder/adpm-build.py \
  --name icloud-cli-full \
  --version 0.1.0 \
  --libraries /opt/homebrew/lib/libimobiledevice* \
  --libraries /opt/homebrew/lib/libusbmuxd* \
  --libraries /opt/homebrew/lib/libplist* \
  --python pymobiledevice3 \
  --python pyicloud

# Creates: adpm/packages/icloud-cli-full-0.1.0.adpm
```

### Installing a Package

```bash
# Method 1: Standalone archive
./adpm/installer/adpm-install.sh icloud-cli-full-0.1.0.adpm

# Method 2: Self-extracting installer
./adpm/builder/make-self-extracting.sh \
  adpm/packages/icloud-cli-full-0.1.0.adpm \
  icloud-cli-installer

./icloud-cli-installer  # One-command install!
```

## Example: Packaging libimobiledevice

```bash
# On macOS ARM64, package homebrew libs
./adpm/builder/adpm-build.py \
  --name libimobiledevice \
  --version 1.3.0 \
  --platform darwin-arm64 \
  --binaries /opt/homebrew/bin/idevice* \
  --libraries /opt/homebrew/lib/libimobiledevice* \
  --libraries /opt/homebrew/lib/libusbmuxd* \
  --libraries /opt/homebrew/lib/libplist* \
  --libraries /opt/homebrew/lib/libssl* \
  --libraries /opt/homebrew/lib/libcrypto*

# On Linux x86_64, package system libs
./adpm/builder/adpm-build.py \
  --name libimobiledevice \
  --version 1.3.0 \
  --platform linux-x86_64 \
  --binaries /usr/bin/idevice* \
  --libraries /usr/lib/x86_64-linux-gnu/libimobiledevice*

# Combine into multi-platform package (advanced)
# TODO: Multi-platform merge tool
```

## Example: iCloud CLI Full Package

Here's how to create a complete iCloud CLI package with all dependencies:

```bash
#!/bin/bash
# build-icloud-cli-package.sh

# Detect current platform
PLATFORM=$(python3 -c "
import platform
s = platform.system().lower()
m = platform.machine().lower()
if s == 'darwin':
    print('darwin-arm64' if m in ['arm64', 'aarch64'] else 'darwin-x86_64')
elif s == 'linux':
    print('linux-aarch64' if m in ['arm64', 'aarch64'] else 'linux-x86_64')
")

echo "Building for platform: $PLATFORM"

# Find libimobiledevice libraries
if [[ "$PLATFORM" == darwin-* ]]; then
    LIB_PATH="/opt/homebrew/lib"
    BIN_PATH="/opt/homebrew/bin"
else
    LIB_PATH="/usr/lib/x86_64-linux-gnu"
    BIN_PATH="/usr/bin"
fi

# Build package
./adpm/builder/adpm-build.py \
  --name icloud-cli-full \
  --version 0.1.0 \
  --platform "$PLATFORM" \
  --binaries "$BIN_PATH"/idevice* \
  --libraries "$LIB_PATH"/libimobiledevice* \
  --libraries "$LIB_PATH"/libusbmuxd* \
  --libraries "$LIB_PATH"/libplist* \
  --python pyicloud \
  --python pymobiledevice3 \
  --python click \
  --python rich \
  --python keyring

# Create self-extracting installer
./adpm/builder/make-self-extracting.sh \
  adpm/packages/icloud-cli-full-0.1.0.adpm \
  icloud-cli-installer

echo "✓ Self-extracting installer ready: ./icloud-cli-installer"
```

## Installation Behavior

When a user runs the installer:

1. **Platform detection** - Automatically detects OS and architecture
2. **Extract** - Unpacks cpio.bz2 archive to temp directory
3. **Install binaries** - Copies platform-specific binaries to `~/.local/bin`
4. **Install libraries** - Copies platform-specific libs to `~/.local/lib`
5. **Install Python packages** - Uses pip to install bundled wheels
6. **Setup** - Runs any post-install scripts
7. **Cleanup** - Removes temp directory

Default install location: `~/.local` (override with `ADPM_PREFIX` env var)

## Advanced Usage

### Custom Install Prefix

```bash
ADPM_PREFIX=/opt/myapp ./installer
```

### Inspecting a Package

```bash
# Extract without installing
bunzip2 -c package.adpm | cpio -idm -D /tmp/inspect

# View metadata
cat /tmp/inspect/META.json | python3 -m json.tool
```

### Building Multi-Platform Packages

```bash
# Build for each platform
for platform in darwin-arm64 darwin-x86_64 linux-x86_64; do
  ./adpm/builder/adpm-build.py \
    --name mypackage \
    --version 1.0.0 \
    --platform $platform \
    --libraries /path/to/$platform/libs \
    --output packages/$platform
done

# TODO: Merge tool to combine platforms into single .adpm
```

## Comparison to Other Solutions

| Feature | pip | brew/apt | ADPM |
|---------|-----|----------|------|
| Python deps | ✓ | Partial | ✓ |
| C library deps | Manual | ✓ | ✓ Bundled |
| Cross-platform | ✓ | ✗ | ✓ |
| Offline install | Partial | ✗ | ✓ |
| No root required | ✓ | ✗ | ✓ |
| Self-extracting | ✗ | ✗ | ✓ |
| Size | Small | N/A | Large |

## Future Enhancements

- [ ] Multi-platform merge tool
- [ ] Delta updates (binary diffs)
- [ ] GPG signature verification
- [ ] Central package repository
- [ ] Dependency resolution between packages
- [ ] Auto-build farm for all platforms

## Philosophy

ADPM doesn't try to replace pip, homebrew, or apt. It's a **distribution format** for complex projects that need to ship with C dependencies intact.

Think of it as:
- **pip** = dependency resolver
- **ADPM** = dependency bundler
- **Together** = happy users

## Credits

Homage to **Todd Bennett III** and the unixeng team for teaching us that sometimes the old Unix ways (cpio archives, shell scripts, platform detection) are still the best ways.

## License

MIT
