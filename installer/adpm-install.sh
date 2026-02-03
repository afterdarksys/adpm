#!/bin/bash
# ADPM Installer - AfterDark Package Manager Installer
# Homage to Todd Bennett III, unixeng
#
# Usage: ./adpm-install.sh package.adpm
#    or: cat adpm-install.sh package.adpm > installer && ./installer

set -e

ADPM_VERSION="0.1.0"
INSTALL_PREFIX="${ADPM_PREFIX:-$HOME/.local}"
TEMP_DIR=$(mktemp -d -t adpm.XXXXXX)

# Cleanup on exit
cleanup() {
    rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

# Colors for output
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    CYAN='\033[0;36m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    CYAN=''
    NC=''
fi

log_info() {
    echo -e "${CYAN}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

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

# Check if this is a self-extracting archive
is_self_extracting() {
    # Check if there's binary data after the script
    local marker="__ADPM_ARCHIVE_BELOW__"
    if grep -q "$marker" "$0" 2>/dev/null; then
        return 0
    fi
    return 1
}

# Extract archive
extract_archive() {
    local archive_file="$1"
    local extract_dir="$2"

    log_info "Extracting archive..."

    if is_self_extracting; then
        # Self-extracting mode
        local marker_line=$(grep -n "^__ADPM_ARCHIVE_BELOW__$" "$0" | cut -d: -f1)
        local archive_start=$((marker_line + 1))
        tail -n +$archive_start "$0" | bunzip2 | (cd "$extract_dir" && cpio -idm)
    else
        # Standalone archive mode
        if [ ! -f "$archive_file" ]; then
            log_error "Archive file not found: $archive_file"
            exit 1
        fi
        bunzip2 -c "$archive_file" | (cd "$extract_dir" && cpio -idm)
    fi

    log_success "Archive extracted"
}

# Read package metadata
read_metadata() {
    local meta_file="$TEMP_DIR/META.json"

    if [ ! -f "$meta_file" ]; then
        log_error "META.json not found in package"
        exit 1
    fi

    # Extract basic info (works without jq)
    PACKAGE_NAME=$(grep '"name"' "$meta_file" | cut -d'"' -f4)
    PACKAGE_VERSION=$(grep '"version"' "$meta_file" | cut -d'"' -f4)

    log_info "Package: $PACKAGE_NAME v$PACKAGE_VERSION"
}

# Install package contents
install_package() {
    local platform="$1"

    log_info "Installing for platform: $platform"

    # Create installation directories
    mkdir -p "$INSTALL_PREFIX"/{bin,lib}

    # Install binaries
    local bin_dir="$TEMP_DIR/bin/$platform"
    if [ -d "$bin_dir" ] && [ "$(ls -A $bin_dir 2>/dev/null)" ]; then
        log_info "Installing binaries to $INSTALL_PREFIX/bin"
        for binary in "$bin_dir"/*; do
            if [ -f "$binary" ]; then
                cp -v "$binary" "$INSTALL_PREFIX/bin/"
                chmod +x "$INSTALL_PREFIX/bin/$(basename $binary)"
            fi
        done
    else
        log_warn "No binaries found for platform $platform"
    fi

    # Install libraries
    local lib_dir="$TEMP_DIR/lib/$platform"
    if [ -d "$lib_dir" ] && [ "$(ls -A $lib_dir 2>/dev/null)" ]; then
        log_info "Installing libraries to $INSTALL_PREFIX/lib"
        for lib in "$lib_dir"/*; do
            if [ -f "$lib" ]; then
                cp -v "$lib" "$INSTALL_PREFIX/lib/"
            fi
        done
    else
        log_warn "No libraries found for platform $platform"
    fi

    # Install Python packages
    if [ -d "$TEMP_DIR/python" ] && command -v pip3 >/dev/null 2>&1; then
        log_info "Installing Python packages..."
        pip3 install --user --no-index --find-links="$TEMP_DIR/python" "$TEMP_DIR/python"/*.whl 2>/dev/null || true
    fi

    # Run installation script if present
    if [ -f "$TEMP_DIR/INSTALL.sh" ]; then
        log_info "Running package installation script..."
        (cd "$TEMP_DIR" && bash INSTALL.sh)
    fi
}

# Main installation flow
main() {
    echo "============================================"
    echo "  ADPM - AfterDark Package Manager v$ADPM_VERSION"
    echo "  Homage to Todd Bennett III, unixeng"
    echo "============================================"
    echo

    # Detect platform
    PLATFORM=$(detect_platform)
    if [ "$PLATFORM" = "unknown" ]; then
        log_error "Unsupported platform: $(uname -s)/$(uname -m)"
        exit 1
    fi

    log_info "Detected platform: $PLATFORM"
    log_info "Install prefix: $INSTALL_PREFIX"
    echo

    # Extract archive
    if is_self_extracting; then
        log_info "Running self-extracting installer"
        extract_archive "" "$TEMP_DIR"
    else
        if [ -z "$1" ]; then
            log_error "Usage: $0 <package.adpm>"
            exit 1
        fi
        extract_archive "$1" "$TEMP_DIR"
    fi

    # Read metadata
    read_metadata

    # Install
    install_package "$PLATFORM"

    echo
    echo "============================================"
    log_success "Installation complete!"
    echo "============================================"
    echo
    echo "Installed to: $INSTALL_PREFIX"
    echo
    echo "Add to your PATH:"
    echo "  export PATH=\"$INSTALL_PREFIX/bin:\$PATH\""
    echo
    echo "Or add to ~/.bashrc or ~/.zshrc:"
    echo "  echo 'export PATH=\"$INSTALL_PREFIX/bin:\$PATH\"' >> ~/.bashrc"
    echo
}

# If not being sourced, run main
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi

exit 0

__ADPM_ARCHIVE_BELOW__
