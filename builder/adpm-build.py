#!/usr/bin/env python3
"""
ADPM Builder - AfterDark Package Manager Builder

Creates .adpm packages (cpio.bz2 archives) with platform-specific binaries.
Homage to Todd Bennett III, unixeng.
"""

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Dict, Optional


class ADPMBuilder:
    """Build ADPM packages from dependencies."""

    SUPPORTED_PLATFORMS = [
        "darwin-arm64",
        "darwin-x86_64",
        "linux-x86_64",
        "linux-aarch64",
    ]

    def __init__(self, name: str, version: str, output_dir: str = "adpm/packages"):
        self.name = name
        self.version = version
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)
        self.temp_dir = None
        self.metadata = {
            "name": name,
            "version": version,
            "packager": "AfterDark Package Manager",
            "platforms": [],
            "dependencies": {},
            "install": {
                "requires_root": False,
                "install_prefix": "~/.local",
                "post_install": []
            }
        }

    def detect_platform(self) -> str:
        """Detect current platform."""
        system = platform.system().lower()
        machine = platform.machine().lower()

        if system == "darwin":
            if machine in ["arm64", "aarch64"]:
                return "darwin-arm64"
            return "darwin-x86_64"
        elif system == "linux":
            if machine in ["arm64", "aarch64"]:
                return "linux-aarch64"
            return "linux-x86_64"

        raise ValueError(f"Unsupported platform: {system}-{machine}")

    def create_staging_dir(self) -> Path:
        """Create temporary staging directory."""
        self.temp_dir = Path(tempfile.mkdtemp(prefix="adpm_build_"))
        (self.temp_dir / "bin").mkdir(exist_ok=True)
        (self.temp_dir / "lib").mkdir(exist_ok=True)
        (self.temp_dir / "python").mkdir(exist_ok=True)
        return self.temp_dir

    def add_binaries(self, binary_paths: List[str], target_platform: str):
        """Add binary executables to package."""
        platform_bin = self.temp_dir / "bin" / target_platform
        platform_bin.mkdir(parents=True, exist_ok=True)

        for binary_path in binary_paths:
            src = Path(binary_path).expanduser()
            if src.is_file():
                shutil.copy2(src, platform_bin / src.name)
                print(f"  Added binary: {src.name}")
            elif src.is_dir():
                # Copy all executables from directory
                for exe in src.glob("*"):
                    if exe.is_file() and os.access(exe, os.X_OK):
                        shutil.copy2(exe, platform_bin / exe.name)
                        print(f"  Added binary: {exe.name}")

    def add_libraries(self, library_paths: List[str], target_platform: str):
        """Add shared libraries to package."""
        platform_lib = self.temp_dir / "lib" / target_platform
        platform_lib.mkdir(parents=True, exist_ok=True)

        for lib_path in library_paths:
            src = Path(lib_path).expanduser()
            if src.is_file():
                shutil.copy2(src, platform_lib / src.name)
                print(f"  Added library: {src.name}")
            elif src.is_dir():
                # Copy all .so/.dylib files
                for pattern in ["*.so*", "*.dylib"]:
                    for lib in src.glob(pattern):
                        if lib.is_file():
                            shutil.copy2(lib, platform_lib / lib.name)
                            print(f"  Added library: {lib.name}")

    def add_python_packages(self, package_names: List[str]):
        """Download and add Python packages (wheels)."""
        python_dir = self.temp_dir / "python"

        for package in package_names:
            print(f"  Downloading Python package: {package}")
            try:
                subprocess.run(
                    ["pip", "download", "--dest", str(python_dir), "--no-deps", package],
                    check=True,
                    capture_output=True
                )
                self.metadata["dependencies"][package] = {
                    "type": "python",
                    "version": "latest"
                }
            except subprocess.CalledProcessError as e:
                print(f"    Warning: Failed to download {package}: {e}")

    def add_dependency_metadata(self, dep_name: str, version: str, platforms: List[str]):
        """Add dependency metadata."""
        self.metadata["dependencies"][dep_name] = {
            "version": version,
            "platforms": platforms
        }

    def create_install_script(self):
        """Create installation script."""
        install_script = '''#!/bin/bash
# ADPM Installation Script
# Homage to Todd Bennett III, unixeng

set -e

PACKAGE_NAME="{{ NAME }}"
PACKAGE_VERSION="{{ VERSION }}"
INSTALL_PREFIX="${ADPM_PREFIX:-$HOME/.local}"

echo "===== ADPM Installer ====="
echo "Package: $PACKAGE_NAME v$PACKAGE_VERSION"
echo "Install to: $INSTALL_PREFIX"
echo

# Detect platform
detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)

    case "$os" in
        darwin)
            case "$arch" in
                arm64|aarch64) echo "darwin-arm64" ;;
                x86_64) echo "darwin-x86_64" ;;
                *) echo "unknown" ;;
            esac
            ;;
        linux)
            case "$arch" in
                aarch64|arm64) echo "linux-aarch64" ;;
                x86_64) echo "linux-x86_64" ;;
                *) echo "unknown" ;;
            esac
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

PLATFORM=$(detect_platform)
echo "Detected platform: $PLATFORM"

if [ "$PLATFORM" = "unknown" ]; then
    echo "Error: Unsupported platform"
    exit 1
fi

# Create install directories
mkdir -p "$INSTALL_PREFIX"/{bin,lib}

# Install binaries
if [ -d "bin/$PLATFORM" ]; then
    echo "Installing binaries..."
    cp -v bin/$PLATFORM/* "$INSTALL_PREFIX/bin/" 2>/dev/null || true
    chmod +x "$INSTALL_PREFIX/bin"/* 2>/dev/null || true
fi

# Install libraries
if [ -d "lib/$PLATFORM" ]; then
    echo "Installing libraries..."
    cp -v lib/$PLATFORM/* "$INSTALL_PREFIX/lib/" 2>/dev/null || true
fi

# Install Python packages
if [ -d "python" ] && command -v pip >/dev/null 2>&1; then
    echo "Installing Python packages..."
    pip install --no-index --find-links=python python/*.whl 2>/dev/null || true
fi

echo
echo "===== Installation Complete ====="
echo "Binaries installed to: $INSTALL_PREFIX/bin"
echo "Libraries installed to: $INSTALL_PREFIX/lib"
echo
echo "Add to PATH:"
echo "  export PATH=\"$INSTALL_PREFIX/bin:\\$PATH\""
echo
'''
        install_script = install_script.replace("{{ NAME }}", self.name)
        install_script = install_script.replace("{{ VERSION }}", self.version)

        install_path = self.temp_dir / "INSTALL.sh"
        install_path.write_text(install_script)
        install_path.chmod(0o755)

    def write_metadata(self):
        """Write META.json file."""
        meta_path = self.temp_dir / "META.json"
        with open(meta_path, 'w') as f:
            json.dump(self.metadata, f, indent=2)

    def build_archive(self) -> Path:
        """Build the final .adpm archive (cpio.bz2)."""
        output_file = self.output_dir / f"{self.name}-{self.version}.adpm"

        print(f"\nBuilding archive: {output_file}")

        # Create cpio archive
        cpio_file = self.temp_dir / "package.cpio"
        subprocess.run(
            f"cd {self.temp_dir} && find . -print | cpio -o > {cpio_file}",
            shell=True,
            check=True
        )

        # Compress with bzip2
        subprocess.run(
            ["bzip2", "-9", str(cpio_file)],
            check=True
        )

        # Move to output directory
        shutil.move(str(cpio_file) + ".bz2", output_file)

        print(f"✓ Package created: {output_file}")
        print(f"  Size: {output_file.stat().st_size / 1024 / 1024:.2f} MB")

        return output_file

    def cleanup(self):
        """Clean up temporary directory."""
        if self.temp_dir and self.temp_dir.exists():
            shutil.rmtree(self.temp_dir)

    def build(self, binaries: List[str] = None, libraries: List[str] = None,
              python_packages: List[str] = None, target_platform: str = None):
        """Build complete package."""
        try:
            if not target_platform:
                target_platform = self.detect_platform()

            print(f"Building ADPM package: {self.name} v{self.version}")
            print(f"Target platform: {target_platform}")

            self.create_staging_dir()
            self.metadata["platforms"].append(target_platform)

            if binaries:
                print("Adding binaries...")
                self.add_binaries(binaries, target_platform)

            if libraries:
                print("Adding libraries...")
                self.add_libraries(libraries, target_platform)

            if python_packages:
                print("Adding Python packages...")
                self.add_python_packages(python_packages)

            self.create_install_script()
            self.write_metadata()

            return self.build_archive()

        finally:
            self.cleanup()


def main():
    parser = argparse.ArgumentParser(
        description="ADPM Builder - Create AfterDark Package Manager packages"
    )
    parser.add_argument("--name", required=True, help="Package name")
    parser.add_argument("--version", required=True, help="Package version")
    parser.add_argument("--platform", help="Target platform (auto-detected if not specified)")
    parser.add_argument("--binaries", nargs="+", help="Binary files or directories to include")
    parser.add_argument("--libraries", nargs="+", help="Library files or directories to include")
    parser.add_argument("--python", nargs="+", help="Python packages to include")
    parser.add_argument("--output", default="adpm/packages", help="Output directory")

    args = parser.parse_args()

    builder = ADPMBuilder(args.name, args.version, args.output)
    builder.build(
        binaries=args.binaries,
        libraries=args.libraries,
        python_packages=args.python,
        target_platform=args.platform
    )


if __name__ == "__main__":
    main()
