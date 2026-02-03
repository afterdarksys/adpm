#!/bin/bash
# Example: Build iCloud CLI ADPM package
# This script packages iCloud CLI with optional pymobiledevice3 dependencies

set -e

# Configuration
PACKAGE_NAME="icloud-cli-full"
PACKAGE_VERSION="0.1.0"
OUTPUT_DIR="adpm/packages"

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
echo "Building iCloud CLI ADPM package for: $PLATFORM"
echo

if [ "$PLATFORM" = "unknown" ]; then
    echo "Error: Unsupported platform"
    exit 1
fi

# Platform-specific library paths
case "$PLATFORM" in
    darwin-arm64|darwin-x86_64)
        # Homebrew paths (might be /usr/local or /opt/homebrew)
        if [ -d "/opt/homebrew" ]; then
            LIB_PREFIX="/opt/homebrew"
        else
            LIB_PREFIX="/usr/local"
        fi

        BINARIES=(
            "$LIB_PREFIX/bin/idevice*"
        )

        LIBRARIES=(
            "$LIB_PREFIX/lib/libimobiledevice*.dylib"
            "$LIB_PREFIX/lib/libusbmuxd*.dylib"
            "$LIB_PREFIX/lib/libplist*.dylib"
            "$LIB_PREFIX/opt/openssl@3/lib/libssl*.dylib"
            "$LIB_PREFIX/opt/openssl@3/lib/libcrypto*.dylib"
        )
        ;;

    linux-x86_64|linux-aarch64)
        BINARIES=(
            "/usr/bin/idevice*"
        )

        LIBRARIES=(
            "/usr/lib/*/libimobiledevice*.so*"
            "/usr/lib/*/libusbmuxd*.so*"
            "/usr/lib/*/libplist*.so*"
        )
        ;;
esac

# Check if libraries exist
echo "Checking for required libraries..."
FOUND_LIBS=false
for lib_pattern in "${LIBRARIES[@]}"; do
    if ls $lib_pattern 2>/dev/null | head -1 >/dev/null; then
        FOUND_LIBS=true
        break
    fi
done

if [ "$FOUND_LIBS" = false ]; then
    echo "Warning: libimobiledevice libraries not found"
    echo "Package will include only Python dependencies"
    echo
    echo "To include local device support, install libimobiledevice:"
    echo "  macOS: brew install libimobiledevice"
    echo "  Linux: apt install libimobiledevice6 libimobiledevice-utils"
    echo
    read -p "Continue without device support? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
    BINARIES=()
    LIBRARIES=()
fi

# Python packages to include
PYTHON_PACKAGES=(
    "pyicloud"
    "pymobiledevice3"
    "click"
    "rich"
    "keyring"
)

# Build the package
echo "Building ADPM package..."
echo

BUILD_ARGS=(
    "--name" "$PACKAGE_NAME"
    "--version" "$PACKAGE_VERSION"
    "--platform" "$PLATFORM"
    "--output" "$OUTPUT_DIR"
)

# Add binaries if found
if [ ${#BINARIES[@]} -gt 0 ]; then
    BUILD_ARGS+=("--binaries" "${BINARIES[@]}")
fi

# Add libraries if found
if [ ${#LIBRARIES[@]} -gt 0 ]; then
    BUILD_ARGS+=("--libraries" "${LIBRARIES[@]}")
fi

# Add Python packages
BUILD_ARGS+=("--python" "${PYTHON_PACKAGES[@]}")

# Run builder
python3 adpm/builder/adpm-build.py "${BUILD_ARGS[@]}"

# Create self-extracting installer
ADPM_FILE="$OUTPUT_DIR/$PACKAGE_NAME-$PACKAGE_VERSION.adpm"
INSTALLER_FILE="icloud-cli-installer-$PLATFORM"

if [ -f "$ADPM_FILE" ]; then
    echo
    echo "Creating self-extracting installer..."
    ./adpm/builder/make-self-extracting.sh "$ADPM_FILE" "$INSTALLER_FILE"

    echo
    echo "✓ Build complete!"
    echo
    echo "Package: $ADPM_FILE"
    echo "Installer: $INSTALLER_FILE"
    echo
    echo "Distribution options:"
    echo "  1. Share .adpm file: Users run: ./adpm/installer/adpm-install.sh $ADPM_FILE"
    echo "  2. Share installer: Users run: ./$INSTALLER_FILE"
    echo
fi
