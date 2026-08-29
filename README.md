ADPM - After Dark Systems Package Manager
=================================

**Homage to Todd Bennett III, unixeng**

A lightweight package manager for bundling complex dependencies (especially C libraries) with Python projects.

The public executable is `adpm` and every operation follows `adpm <command> [options]`.

```bash
./build.sh
./adpm --help
./adpm inspect package.adpm
./adpm validate package.adpm
./adpm status
./adpm history
```

## Package Conversion

`adpm convert` reads and writes RPM, DEB, TGZ, BPM, ADPM, raw CPIO,
gzip CPIO, and bzip2 CPIO. `tar.gz` is accepted as an alias for `tgz`.

```bash
adpm convert app.rpm --inpkg rpm --outpkg adpm --output dist/
adpm convert app.adpm --inpkg adpm --outpkg deb --output dist/
adpm convert payload.cpio.gz --inpkg cpio.gz --outpkg tgz --output payload.tgz
```

RPM input requires `rpm2cpio`; RPM and DEB output require `fpm`. BPM has
no universal archive standard, so input is detected as tar or CPIO and ADPM
emits BPM as gzip-compressed SVR4 `newc` CPIO.

Platform packages can be combined without rebuilding:

```bash
adpm merge darwin.adpm linux.adpm --output app-universal.adpm
```

## Native Dependency Portability

ADPM can recursively inspect ELF and Mach-O payloads, bundle non-system shared
libraries, and rewrite loader paths on the staged copies:

```bash
adpm build --name app --version 1.0.0 --binaries ./app \
  --detect-dependencies --relocate
```

Linux discovery uses `ldd` with an optional `readelf` fallback; relocation
requires `patchelf`. macOS discovery uses `otool -L`, including `LC_RPATH`
resolution, and relocation uses `install_name_tool`. `adpm validate` rejects a
package whose native-dependency report contains unresolved libraries.

## Tests

Run the complete builder, conversion, archive, and installer suite with:

```bash
tests/run.sh
```

## Lifecycle State and Ownership

ADPM keeps current installations, durable lifecycle history, build records, and
file ownership under `${ADPM_DB:-~/.local/share/adpm}`. Builds capture source
provenance and the archive SHA-256; installs retain that provenance and record
the exact files copied. Removal deletes the current installed record but leaves
the append-only history intact.

```bash
adpm status                 # all currently installed packages
adpm status package-name    # one current installation record
adpm history                # builds, installs, upgrades, rollbacks, removals
adpm history package-name   # history for one package
```

Before copying files, the installer rejects paths owned by another ADPM package.
Uninstall only removes paths still owned by that package. Existing 0.3.0
installed records remain readable and gain ownership state on their next
install or upgrade.

## Configuration and Automation

ADPM reads YAML configuration (with a `.cfg` extension) from `/etc/adpm/` and
then `$HOME/.adpm/`. `adpm.cfg` controls CLI paths and command defaults;
`adpm_auto.cfg` supplies non-interactive policy, reusable answers, and
automation-specific defaults. User values override system values, while
`--config`, `--auto-config`, `ADPM_CONFIG`, and `ADPM_AUTO_CONFIG` can select
additional higher-precedence files.

```yaml
# ~/.adpm/adpm.cfg
database: ~/.local/share/adpm
prefix: ~/.local
defaults:
  build:
    compress: zstd
  install:
    verify: true
```

```yaml
# ~/.adpm/adpm_auto.cfg
enabled: true
non_interactive: true
assume_yes: false
answers:
  replace_modified_file: false
```

Use `adpm config` to inspect the effective merged configuration and its source
files. When enabled, resolved automation policy and answers are passed to ADPM
subprocesses as environment data, not executable snippets. Automation settings
never disable package verification, dependency, platform, or ownership safety
checks.

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

`.adpm` files are compressed SVR4 `newc` CPIO archives (bzip2 by default;
gzip, xz, and zstd are supported) containing:

```
package.adpm (cpio.bz2)
├── META.json           # Package metadata
├── INSTALL.sh          # Installation logic
├── .ADPM_STATE.py      # Embedded lifecycle support for self-extracting use
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

### Why compressed CPIO?

- **Standard Unix format** - Available everywhere
- **Better than tar** for special files
- **Good compression** - bzip2 is widely available
- **Easy scripting** - Standard tools

## Quick Start

### Building a Package

```bash
# Build package with libimobiledevice binaries
./builder/adpm-build.py \
  --name icloud-cli-full \
  --version 0.1.0 \
  --libraries /opt/homebrew/lib/libimobiledevice* \
  --libraries /opt/homebrew/lib/libusbmuxd* \
  --libraries /opt/homebrew/lib/libplist* \
  --python pymobiledevice3 \
  --python pyicloud

# Creates: dist/icloud-cli-full-0.1.0.adpm
```

### Installing a Package

```bash
# Method 1: Standalone archive
adpm install icloud-cli-full-0.1.0.adpm

# Method 2: Self-extracting installer
./builder/make-self-extracting.sh \
  dist/icloud-cli-full-0.1.0.adpm \
  icloud-cli-installer

./icloud-cli-installer  # One-command install!
```

## Example: Packaging libimobiledevice

```bash
# On macOS ARM64, package homebrew libs
./builder/adpm-build.py \
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
./builder/adpm-build.py \
  --name libimobiledevice \
  --version 1.3.0 \
  --platform linux-x86_64 \
  --binaries /usr/bin/idevice* \
  --libraries /usr/lib/x86_64-linux-gnu/libimobiledevice*

# Combine platform packages
adpm merge dist/libimobiledevice-darwin.adpm dist/libimobiledevice-linux.adpm \
  --output dist/libimobiledevice-universal.adpm
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
./builder/adpm-build.py \
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
./builder/make-self-extracting.sh \
  dist/icloud-cli-full-0.1.0.adpm \
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
  ./builder/adpm-build.py \
    --name mypackage \
    --version 1.0.0 \
    --platform $platform \
    --libraries /path/to/$platform/libs \
    --output packages/$platform
done

adpm merge packages/*/*.adpm --output packages/mypackage-universal.adpm
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
