# ADPM - AfterDark Package Manager

**Format:** cpio.bz2 archives with metadata for cross-platform dependency management

**Homage to:** Todd Bennett III, unixeng

## Package Format Specification

### Structure

```
package.adpm (cpio.bz2 archive)
├── META.json              # Package metadata
├── INSTALL.sh             # Installation script
├── bin/                   # Compiled binaries (platform-specific)
│   ├── darwin-arm64/
│   ├── darwin-x86_64/
│   ├── linux-x86_64/
│   └── linux-aarch64/
├── lib/                   # Shared libraries
│   ├── darwin-arm64/
│   ├── darwin-x86_64/
│   ├── linux-x86_64/
│   └── linux-aarch64/
└── python/                # Python packages (wheels)
    └── *.whl
```

### META.json Format

```json
{
  "name": "icloud-cli-full",
  "version": "0.1.0",
  "description": "iCloud CLI with all dependencies",
  "packager": "AfterDark Package Manager",
  "platforms": ["darwin-arm64", "darwin-x86_64", "linux-x86_64", "linux-aarch64"],
  "dependencies": {
    "libimobiledevice": {
      "version": "1.3.0",
      "platforms": ["darwin-arm64", "darwin-x86_64", "linux-x86_64"]
    },
    "pymobiledevice3": {
      "version": "4.0.0",
      "type": "python"
    }
  },
  "install": {
    "requires_root": false,
    "install_prefix": "~/.local",
    "post_install": ["echo 'Add ~/.local/bin to PATH'"]
  }
}
```

## Usage

### Building a Package

```bash
./adpm/builder/adpm-build.py \
  --name icloud-cli-full \
  --version 0.1.0 \
  --platform darwin-arm64 \
  --include-binaries /opt/homebrew/lib/libimobiledevice* \
  --include-python pymobiledevice3 \
  --output packages/icloud-cli-full.adpm
```

### Installing a Package

```bash
./adpm/installer/adpm-install.sh icloud-cli-full.adpm
```

Auto-detects platform and extracts appropriate binaries.

### Self-Extracting Archive

```bash
# Create self-extracting installer
cat adpm/installer/adpm-install.sh icloud-cli-full.adpm > icloud-cli-installer
chmod +x icloud-cli-installer

# User just runs:
./icloud-cli-installer
```

## Design Philosophy

**Problem:** Python packages with C dependencies suck to install
- libimobiledevice requires compilation
- Different paths on different systems
- Homebrew, apt, manual builds all different

**Solution:** Ship pre-compiled binaries in a smart archive
- Platform detection at runtime
- Extract only what's needed for current platform
- Self-contained installations
- No root required (installs to ~/.local by default)

**Format choice: cpio.bz2**
- Standard Unix archive format
- Better handling of special files than tar
- bzip2 compression (good ratio, widely available)
- Easy to script with standard tools

## Advantages Over pip

| Feature | pip | ADPM |
|---------|-----|------|
| Pure Python deps | ✓ | ✓ |
| C library deps | Compile or fail | ✓ Pre-compiled |
| Offline install | Partial | ✓ Everything bundled |
| Platform detection | Manual | ✓ Automatic |
| Self-extracting | ✗ | ✓ Optional |
| Versioned libs | System dependent | ✓ Vendored |

## Compatibility with pip

ADPM doesn't replace pip - it complements it:

```bash
# Option 1: Use pip (requires system deps)
pip install icloud-cli[full]

# Option 2: Use ADPM (zero external deps)
./icloud-cli-installer.adpm
```

Both result in working `icloud` command, but ADPM handles the C library hell.

## Future Enhancements

- **Dependency resolution:** Multiple .adpm packages with shared deps
- **Delta updates:** Binary diffs for package updates
- **Signature verification:** GPG signing for security
- **Repository support:** Central package registry
- **Build farm:** Auto-compile for all platforms
