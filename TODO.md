# TODO

## Core Features
- [x] Multi-platform merge tool to combine separate platform builds into single .adpm
- [x] Dependency checks and version constraint detection for installed ADPM packages
- [ ] Delta updates using binary diffs for package updates
- [x] GPG signature and SHA-256 verification for security

## Repository & Distribution
- [ ] Central package repository
- [ ] Auto-build farm for all supported platforms
- [x] Package search and discovery tools for local catalogs
- [x] Installed dependency version conflict detection

## Builder Improvements
- [x] Automatic recursive dependency detection for ELF and Mach-O binaries
- [x] RPATH/install-name rewriting for relocated Linux and macOS libraries
- [x] Strip debug symbols option to reduce size
- [x] Compression options (bzip2, gzip, xz, zstd), including modern DEB payload input

## Installer Enhancements
- [x] Uninstall plus one-generation rollback snapshots for upgrades
- [x] Upgrade existing installations
- [x] System-wide installation option
- [x] Package verification before installation

## Documentation
- [ ] Complete API documentation
- [ ] More real-world examples
- [ ] Troubleshooting guide
- [ ] Platform-specific build guides

## Testing
- [x] Unit tests for builder
- [x] Integration tests for install, upgrade, rollback, dependency, verification, and platform rejection
- [ ] Cross-platform testing automation
- [x] Package validation and inspection tools
- [x] Archive round-trip and traversal-security unit tests
- [x] Portable and native conversion-matrix tests
