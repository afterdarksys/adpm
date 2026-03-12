#!/bin/bash
# ADPM Installer - AfterDark Package Manager Installer
# Homage to Todd Bennett III, unixeng
#
# Usage:
#   ./adpm-install.sh package.adpm          # Install
#   ./adpm-install.sh --uninstall PACKAGE   # Remove installed package
#   ./adpm-install.sh --upgrade package.adpm # Upgrade (uninstall old, install new)
#   ./adpm-install.sh --list                # List installed packages
#
#   Self-extracting mode (cat installer + archive > ./installer):
#   ./installer

set -e

ADPM_VERSION="0.2.0"
INSTALL_PREFIX="${ADPM_PREFIX:-$HOME/.local}"
ADPM_DB="${ADPM_DB:-$HOME/.local/share/adpm}"  # Package registry
TEMP_DIR=$(mktemp -d -t adpm.XXXXXX)

cleanup() {
    rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

# ── Colors (TTY only) ─────────────────────────────────────────
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' CYAN='' BOLD='' NC=''
fi

log_info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC}   $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERR]${NC}  $1" >&2; }

# ── Platform detection ────────────────────────────────────────
detect_platform() {
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$os" in
        darwin)
            case "$arch" in
                arm64|aarch64) echo "darwin-arm64" ;;
                x86_64)        echo "darwin-x86_64" ;;
                *)             echo "unknown" ;;
            esac ;;
        linux)
            case "$arch" in
                aarch64|arm64) echo "linux-aarch64" ;;
                x86_64)        echo "linux-x86_64" ;;
                *)             echo "unknown" ;;
            esac ;;
        *)
            echo "unknown" ;;
    esac
}

# ── Self-extracting detection ─────────────────────────────────
is_self_extracting() {
    local max_lines
    max_lines=$(wc -l < "$0" | tr -d ' ')
    local marker_line
    marker_line=$(grep -a -n "^__ADPM_ARCHIVE_BELOW__$" "$0" 2>/dev/null | cut -d: -f1 | tail -n1)
    if [ -n "$marker_line" ] && [ "$max_lines" -gt "$marker_line" ]; then
        return 0
    fi
    return 1
}

# ── Archive extraction ────────────────────────────────────────
extract_archive() {
    local archive_file="$1"
    local extract_dir="$2"

    log_info "Extracting archive..."

    if is_self_extracting; then
        local marker_line
        marker_line=$(grep -a -n "^__ADPM_ARCHIVE_BELOW__$" "$0" | cut -d: -f1 | tail -n1)
        tail -n +"$((marker_line + 1))" "$0" > "$extract_dir/payload.tmp"
        archive_file="$extract_dir/payload.tmp"
    fi

    [ -f "$archive_file" ] || { log_error "Archive not found: $archive_file"; exit 1; }

    # Detect compression type
    local file_type=""
    if command -v file >/dev/null 2>&1; then
        file_type=$(file "$archive_file" 2>/dev/null)
    fi

    # Pre-flight Path Traversal (Zip Slip) check
    local list_cmd=""
    if echo "$file_type" | grep -qi "xz" || xz -t "$archive_file" 2>/dev/null; then
        list_cmd="unxz -c \"$archive_file\" | cpio -it 2>/dev/null"
        extract_cmd="unxz -c \"$archive_file\" | (cd \"$extract_dir\" && cpio -idm --quiet 2>/dev/null)"
    elif echo "$file_type" | grep -qi "gzip" || gzip -t "$archive_file" 2>/dev/null; then
        list_cmd="gunzip -c \"$archive_file\" | cpio -it 2>/dev/null"
        extract_cmd="gunzip -c \"$archive_file\" | (cd \"$extract_dir\" && cpio -idm --quiet 2>/dev/null)"
    else
        # Default to bzip2 as per original code
        list_cmd="bunzip2 -c \"$archive_file\" | cpio -it 2>/dev/null"
        extract_cmd="bunzip2 -c \"$archive_file\" | (cd \"$extract_dir\" && cpio -idm --quiet 2>/dev/null)"
    fi

    if eval "$list_cmd" | grep -qE '^/|\.\./'; then
        log_error "SECURITY VIOLATION: Archive contains absolute paths or directory traversal (../) payloads!"
        exit 1
    fi

    eval "$extract_cmd"

    if is_self_extracting; then
        rm -f "$archive_file"
    fi

    log_success "Extracted"
}

# ── Metadata ──────────────────────────────────────────────────
read_metadata() {
    local meta_file="$TEMP_DIR/META.json"
    [ -f "$meta_file" ] || { log_error "META.json missing from package"; exit 1; }

    PACKAGE_NAME=$(grep '"name"'    "$meta_file" | head -1 | cut -d'"' -f4)
    PACKAGE_VERSION=$(grep '"version"' "$meta_file" | head -1 | cut -d'"' -f4)

    # Validate package name to prevent local file overwrite attacks
    if [[ "$PACKAGE_NAME" == *"/"* ]] || [[ "$PACKAGE_NAME" == *".."* ]] || [[ "$PACKAGE_VERSION" == *"/"* ]]; then
        log_error "SECURITY VIOLATION: Invalid package name or version!"
        exit 1
    fi

    log_info "Package: ${BOLD}${PACKAGE_NAME} v${PACKAGE_VERSION}${NC}"
}

# ── Package DB ────────────────────────────────────────────────
db_record_install() {
    local name="$1" version="$2" prefix="$3" files_json="$4"
    mkdir -p "$ADPM_DB/installed"
    cat > "$ADPM_DB/installed/${name}.json" <<JSON
{
  "name": "${name}",
  "version": "${version}",
  "prefix": "${prefix}",
  "installed_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "files": ${files_json:-[]}
}
JSON
}

db_record_remove() {
    local name="$1"
    rm -f "$ADPM_DB/installed/${name}.json"
}

db_get_installed() {
    local name="$1"
    local meta="$ADPM_DB/installed/${name}.json"
    [ -f "$meta" ] && cat "$meta" || echo ""
}

db_list_installed() {
    local db_dir="$ADPM_DB/installed"
    if [ ! -d "$db_dir" ] || [ -z "$(ls -A "$db_dir" 2>/dev/null)" ]; then
        echo "No packages installed via ADPM."
        return
    fi
    echo "Installed ADPM packages:"
    echo ""
    for f in "$db_dir"/*.json; do
        local n v p
        n=$(grep '"name"'    "$f" | cut -d'"' -f4)
        v=$(grep '"version"' "$f" | cut -d'"' -f4)
        p=$(grep '"prefix"'  "$f" | cut -d'"' -f4)
        echo "  ${BOLD}${n}${NC} v${v}   (${p})"
    done
    echo ""
}

# ── Verification ──────────────────────────────────────────────
verify_signature() {
    local archive_file="$1"
    local required="$2"
    local sig_file="${archive_file}.asc"
    
    if [ ! -f "$sig_file" ]; then
        if [ "$required" = "1" ]; then
            log_error "Signature file missing ($sig_file) but verification is REQUIRED."
            exit 1
        else
            log_warn "Signature file missing ($sig_file). Skipping verification."
            return 0
        fi
    fi
    
    log_info "Verifying signature: $sig_file"
    
    local gpg_cmd="gpg --verify"
    if [ -n "$ADPM_TRUSTED_KEYS" ] && [ -f "$ADPM_TRUSTED_KEYS" ]; then
        gpg_cmd="gpg --no-default-keyring --keyring $ADPM_TRUSTED_KEYS --verify"
    fi
    
    if $gpg_cmd "$sig_file" "$archive_file" >/dev/null 2>&1; then
        log_success "Signature verified successfully"
    else
        log_error "Signature verification FAILED!"
        exit 1
    fi
}

# ── Install ───────────────────────────────────────────────────
install_package() {
    local platform="$1"
    local files_json="[]"
    local installed_files=""

    log_info "Installing for platform: $platform"
    mkdir -p "$INSTALL_PREFIX"/{bin,lib}

    # Binaries
    local bin_dir="$TEMP_DIR/bin/$platform"
    if [ -d "$bin_dir" ] && [ "$(ls -A "$bin_dir" 2>/dev/null)" ]; then
        log_info "Installing binaries → $INSTALL_PREFIX/bin"
        for binary in "$bin_dir"/*; do
            [ -f "$binary" ] || continue
            cp "$binary" "$INSTALL_PREFIX/bin/"
            chmod +x "$INSTALL_PREFIX/bin/$(basename "$binary")"
            log_success "  $(basename "$binary")"
            installed_files="${installed_files}\"$INSTALL_PREFIX/bin/$(basename "$binary")\","
        done
    else
        log_warn "No binaries for platform $platform"
    fi

    # Libraries
    local lib_dir="$TEMP_DIR/lib/$platform"
    if [ -d "$lib_dir" ] && [ "$(ls -A "$lib_dir" 2>/dev/null)" ]; then
        log_info "Installing libraries → $INSTALL_PREFIX/lib"
        for lib in "$lib_dir"/*; do
            [ -f "$lib" ] || continue
            cp "$lib" "$INSTALL_PREFIX/lib/"
            log_success "  $(basename "$lib")"
            installed_files="${installed_files}\"$INSTALL_PREFIX/lib/$(basename "$lib")\","
        done
    fi

    # Python packages
    if [ -d "$TEMP_DIR/python" ] && command -v pip3 >/dev/null 2>&1; then
        local wheels
        wheels=$(ls "$TEMP_DIR/python"/*.whl 2>/dev/null || true)
        if [ -n "$wheels" ]; then
            log_info "Installing Python packages..."
            pip3 install --user --no-index --find-links="$TEMP_DIR/python" \
                "$TEMP_DIR/python"/*.whl 2>/dev/null || true
        fi
    fi

    # Custom install script
    if [ -f "$TEMP_DIR/INSTALL.sh" ]; then
        log_info "Running package install script..."
        (cd "$TEMP_DIR" && bash INSTALL.sh)
    fi

    # Clean up trailing comma for json
    if [ -n "$installed_files" ]; then
        files_json="[${installed_files%,}]"
    fi

    # Record in DB
    db_record_install "$PACKAGE_NAME" "$PACKAGE_VERSION" "$INSTALL_PREFIX" "$files_json"
}

# ── Uninstall ─────────────────────────────────────────────────
uninstall_package() {
    local name="$1"

    echo ""
    echo "============================================"
    echo "  ADPM Uninstall: $name"
    echo "============================================"
    echo ""

    local info
    info=$(db_get_installed "$name")
    if [ -z "$info" ]; then
        log_warn "Package '$name' is not recorded in the ADPM database."
        log_warn "If it was installed manually, remove the binary from your PATH."
        exit 1
    fi

    local prefix
    prefix=$(echo "$info" | grep '"prefix"' | cut -d'"' -f4)

    # read files array if it exists
    local files_list=""
    if command -v python3 >/dev/null 2>&1; then
        files_list=$(echo "$info" | python3 -c 'import sys, json; data=json.loads(sys.stdin.read()); print("\n".join(data.get("files", [])))' 2>/dev/null || true)
    fi

    if [ -n "$files_list" ]; then
        log_info "Removing tracked files..."
        while IFS= read -r f; do
            if [ -n "$f" ] && [ -f "$f" ]; then
                rm -f "$f"
                log_success "Removed: $f"
            fi
        done <<< "$files_list"
    else
        log_info "Removing $name heuristics from $prefix/bin ..."

        # Remove binaries named after the package
        # Also check for exact binary name match
        for candidate in "$prefix/bin/$name" "$prefix/bin/${name}-cli"; do
            if [ -f "$candidate" ]; then
                rm -f "$candidate"
                log_success "Removed: $candidate"
            fi
        done

        # Prompt for any other binaries that might belong to this package
        log_warn "If other binaries were installed, remove them manually from $prefix/bin"
    fi

    db_record_remove "$name"
    log_success "Package '$name' uninstalled."
    echo ""
}

# ── Upgrade ───────────────────────────────────────────────────
upgrade_package() {
    local archive="$1"

    # Extract to get the package name, then uninstall old version
    log_info "Preparing upgrade..."

    extract_archive "$archive" "$TEMP_DIR"
    read_metadata

    local existing
    existing=$(db_get_installed "$PACKAGE_NAME")
    if [ -n "$existing" ]; then
        local old_version
        old_version=$(echo "$existing" | grep '"version"' | cut -d'"' -f4)
        log_info "Upgrading $PACKAGE_NAME: v$old_version → v$PACKAGE_VERSION"
        uninstall_package "$PACKAGE_NAME"
    else
        log_info "No existing installation found — performing fresh install"
    fi

    # Re-extract (uninstall_package cleanup already happened)
    # We already extracted above, so just install
    local platform
    platform=$(detect_platform)
    [ "$platform" = "unknown" ] && { log_error "Unsupported platform"; exit 1; }
    install_package "$platform"
}

# ── Main ──────────────────────────────────────────────────────
main() {
    local mode="install"
    local is_system=0
    local verify=0
    local verify_req=0

    # Quick parse for flags
    local new_args=()
    for arg in "$@"; do
        if [ "$arg" = "--system" ]; then
            is_system=1
        elif [ "$arg" = "--verify" ]; then
            verify=1
        elif [ "$arg" = "--verify-required" ]; then
            verify=1
            verify_req=1
        else
            new_args+=("$arg")
        fi
    done
    
    set -- "${new_args[@]}"
    local arg1="${1:-}"

    if [ "$is_system" -eq 1 ]; then
        if [ "$(id -u)" -ne 0 ]; then
            log_error "--system requires root privileges"
            exit 1
        fi
        INSTALL_PREFIX="/usr/local"
        ADPM_DB="/var/lib/adpm"
    fi

    case "$arg1" in
        --list)
            mkdir -p "$ADPM_DB/installed"
            db_list_installed
            exit 0
            ;;
        --uninstall)
            [ -z "${2:-}" ] && { log_error "Usage: $0 --uninstall PACKAGE_NAME"; exit 1; }
            mode="uninstall"
            ;;
        --upgrade)
            [ -z "${2:-}" ] && { log_error "Usage: $0 --upgrade package.adpm"; exit 1; }
            mode="upgrade"
            ;;
        --help|-h)
            echo "ADPM Installer v$ADPM_VERSION"
            echo ""
            echo "Usage:"
            echo "  $0 package.adpm               # Install package"
            echo "  $0 --uninstall PACKAGE        # Uninstall package"
            echo "  $0 --upgrade package.adpm     # Upgrade package"
            echo "  $0 --list                     # List installed packages"
            echo "  $0 --system package.adpm      # Install system-wide (requires root)"
            echo "  $0 --verify package.adpm      # Verify package signature before install"
            echo "  $0 --verify-required p.adpm   # Strict variant of verify"
            echo ""
            echo "Environment:"
            echo "  ADPM_PREFIX   Install location (default: ~/.local)"
            echo "  ADPM_DB       Package DB location (default: ~/.local/share/adpm)"
            echo ""
            exit 0
            ;;
    esac

    echo "============================================"
    echo "  ADPM - AfterDark Package Manager v$ADPM_VERSION"
    echo "  Homage to Todd Bennett III, unixeng"
    echo "============================================"
    echo ""

    case "$mode" in
        uninstall)
            uninstall_package "$2"
            exit 0
            ;;
        upgrade)
            [ "$verify" -eq 1 ] && verify_signature "$2" "$verify_req"
            upgrade_package "$2"
            ;;
        install)
            local target_arch=""
            if is_self_extracting; then
                log_info "Self-extracting installer"
            else
                [ -z "$arg1" ] && { log_error "Usage: $0 <package.adpm>"; exit 1; }
                target_arch="$arg1"
            fi
            
            if [ -n "$target_arch" ] && [ "$verify" -eq 1 ]; then
                verify_signature "$target_arch" "$verify_req"
            fi
            
            extract_archive "$target_arch" "$TEMP_DIR"

            read_metadata

            local platform
            platform=$(detect_platform)
            if [ "$platform" = "unknown" ]; then
                log_error "Unsupported platform: $(uname -s)/$(uname -m)"
                exit 1
            fi

            log_info "Platform: $platform"
            log_info "Prefix:   $INSTALL_PREFIX"
            echo ""

            install_package "$platform"
            ;;
    esac

    echo ""
    echo "============================================"
    log_success "Done!"
    echo "============================================"
    echo ""
    echo "Installed to: $INSTALL_PREFIX"
    echo ""
    echo "If $INSTALL_PREFIX/bin is not in your PATH, add:"
    echo "  export PATH=\"$INSTALL_PREFIX/bin:\$PATH\""
    echo ""
    echo "To uninstall later:"
    echo "  adpm-install.sh --uninstall $PACKAGE_NAME"
    echo ""
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi

exit 0

__ADPM_ARCHIVE_BELOW__
