# Changelog

All notable changes to ADPM will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Public `adpm <command> [options]` CLI for After Dark Systems Package Manager
- Bidirectional RPM, DEB, TGZ, BPM, ADPM, and CPIO conversion
- `inspect`, `validate`, and multi-platform `merge` commands
- Dependency constraint checks during install and upgrade
- Secure native tar and CPIO extraction with traversal protection
- Recursive ELF/Mach-O dependency detection and non-system library bundling
- Linux `patchelf` RPATH and macOS `install_name_tool` relocation
- Builder unit, conversion-matrix, and installer lifecycle test suites
- Initial ADPM package manager implementation
- `adpm-build.py` - Package builder script
- `adpm-install.sh` - Package installer script
- `make-self-extracting.sh` - Self-extracting installer creator
- Support for multi-platform binary distribution (darwin-arm64, darwin-x86_64, linux-x86_64, linux-aarch64)
- cpio.bz2 archive format for package distribution
- META.json package metadata format
- Python wheel bundling support
- Example build script for iCloud CLI
- Comprehensive documentation (README.md, ADPM_SPEC.md)

### Homage
- Dedicated to Todd Bennett III and the unixeng team for their Unix wisdom

## [0.1.0] - TBD

Initial release (planned)
