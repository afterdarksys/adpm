#!/bin/bash
# ADPM Installer - After Dark Systems Package Manager Installer
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

ADPM_VERSION="0.3.0"
INSTALL_PREFIX="${ADPM_PREFIX:-$HOME/.local}"
ADPM_DB="${ADPM_DB:-$HOME/.local/share/adpm}"  # Package registry
STATE_TOOL="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/adpm_state.py"
CURRENT_ARCHIVE=""
CURRENT_ARCHIVE_SHA256=""
INSTALL_ACTION="install"
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
    CURRENT_ARCHIVE_SHA256=$(archive_checksum "$archive_file")

    # Detect compression type
    local file_type=""
    if command -v file >/dev/null 2>&1; then
        file_type=$(file "$archive_file" 2>/dev/null)
    fi

    # Pre-flight path and symlink traversal check. Avoid eval so package paths
    # containing shell metacharacters remain data, never commands.
    local compression="bzip2"
    if echo "$file_type" | grep -qi "zstandard" || (command -v zstd >/dev/null 2>&1 && zstd -t "$archive_file" >/dev/null 2>&1); then
        compression="zstd"
    elif echo "$file_type" | grep -qi "xz" || xz -t "$archive_file" 2>/dev/null; then
        compression="xz"
    elif echo "$file_type" | grep -qi "gzip" || gzip -t "$archive_file" 2>/dev/null; then
        compression="gzip"
    fi

    local contents_file="$extract_dir/.adpm-contents"
    case "$compression" in
        zstd) zstd -dc "$archive_file" | cpio -itv > "$contents_file" 2>/dev/null ;;
        xz)    unxz -c "$archive_file" | cpio -itv > "$contents_file" 2>/dev/null ;;
        gzip)  gunzip -c "$archive_file" | cpio -itv > "$contents_file" 2>/dev/null ;;
        *)     bunzip2 -c "$archive_file" | cpio -itv > "$contents_file" 2>/dev/null ;;
    esac || { log_error "Invalid or unsupported package archive"; exit 1; }

    if grep -qE '(^| -> )/|\.\./' "$contents_file"; then
        log_error "SECURITY VIOLATION: Archive contains absolute paths or directory traversal (../) payloads!"
        exit 1
    fi
    rm -f "$contents_file"

    case "$compression" in
        zstd) zstd -dc "$archive_file" | (cd "$extract_dir" && cpio -idm --quiet 2>/dev/null) ;;
        xz)    unxz -c "$archive_file" | (cd "$extract_dir" && cpio -idm --quiet 2>/dev/null) ;;
        gzip)  gunzip -c "$archive_file" | (cd "$extract_dir" && cpio -idm --quiet 2>/dev/null) ;;
        *)     bunzip2 -c "$archive_file" | (cd "$extract_dir" && cpio -idm --quiet 2>/dev/null) ;;
    esac || { log_error "Package extraction failed"; exit 1; }

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
    if ! [[ "$PACKAGE_NAME" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] ||
       ! [[ "$PACKAGE_VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._+:-]*$ ]]; then
        log_error "SECURITY VIOLATION: Invalid package name or version!"
        exit 1
    fi

    log_info "Package: ${BOLD}${PACKAGE_NAME} v${PACKAGE_VERSION}${NC}"
}

# Resolve declared ADPM dependencies against the local package database.
check_dependencies() {
    local meta_file="$TEMP_DIR/META.json"
    command -v python3 >/dev/null 2>&1 || {
        log_warn "python3 unavailable; cannot evaluate package dependencies"
        return 0
    }
    python3 - "$meta_file" "$ADPM_DB/installed" <<'PY'
import json, operator, pathlib, re, sys

meta = json.load(open(sys.argv[1]))
db = pathlib.Path(sys.argv[2])
failed = []

def version(value):
    parts = [(0, int(x)) if x.isdigit() else (1, x.lower())
             for x in re.split(r"[._+-]", value)]
    while parts and parts[-1] == (0, 0):
        parts.pop()
    return tuple(parts)

for name, spec in meta.get("dependencies", {}).items():
    if not isinstance(spec, dict) or spec.get("type") == "python":
        continue
    record = db / (name + ".json")
    if not record.exists():
        failed.append(f"missing dependency: {name}")
        continue
    installed = str(json.load(open(record)).get("version", ""))
    constraint = str(spec.get("version", "*")).strip()
    if constraint in ("", "*", "latest"):
        continue
    match = re.match(r"^(>=|<=|==|=|>|<)?\s*(.+)$", constraint)
    op, wanted = match.groups()
    op = op or "="
    comparisons = {"=": operator.eq, "==": operator.eq, ">=": operator.ge,
                   "<=": operator.le, ">": operator.gt, "<": operator.lt}
    if not comparisons[op](version(installed), version(wanted)):
        failed.append(f"version conflict: {name} {installed} does not satisfy {constraint}")

if failed:
    for failure in failed:
        print("[ERR]  " + failure, file=sys.stderr)
    sys.exit(1)
PY
}

check_platform() {
    local platform="$1"
    command -v python3 >/dev/null 2>&1 || return 0
    python3 - "$TEMP_DIR/META.json" "$platform" <<'PY'
import json, sys
platforms = json.load(open(sys.argv[1])).get("platforms", [])
current = sys.argv[2]
if platforms and current not in platforms and "all" not in platforms and "platform-agnostic" not in platforms:
    print(f"[ERR]  package supports {', '.join(platforms)}, not {current}", file=sys.stderr)
    sys.exit(1)
PY
}

resolve_state_tool() {
    if [ ! -f "$STATE_TOOL" ] && [ ! -f "$TEMP_DIR/.ADPM_STATE.py" ] && is_self_extracting; then
        extract_archive "" "$TEMP_DIR"
    fi
    if [ ! -f "$STATE_TOOL" ] && [ -f "$TEMP_DIR/.ADPM_STATE.py" ]; then
        STATE_TOOL="$TEMP_DIR/.ADPM_STATE.py"
    fi
    [ -f "$STATE_TOOL" ] || { log_error "ADPM state manager not found: $STATE_TOOL"; exit 1; }
}

archive_checksum() {
    local archive="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$archive" | awk '{print $1}'
    else
        shasum -a 256 "$archive" | awk '{print $1}'
    fi
}

plan_install_files() {
    local platform="$1"
    python3 - "$TEMP_DIR" "$INSTALL_PREFIX" "$platform" <<'PY'
import json, pathlib, sys
source, prefix, platform = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), sys.argv[3]
files = []
for kind in ("bin", "lib"):
    directory = source / kind / platform
    if directory.is_dir():
        files.extend(str(prefix / kind / item.name) for item in directory.iterdir() if item.is_file())
payload = source / "payload" / platform
if payload.is_dir():
    for item in payload.rglob("*"):
        if item.is_dir():
            continue
        relative = item.relative_to(payload)
        parts = relative.parts
        if parts[:2] == ("usr", "local"):
            relative = pathlib.Path(*parts[2:])
        elif parts[:1] == ("usr",):
            relative = pathlib.Path(*parts[1:])
        files.append(str(prefix / relative))
print(json.dumps(sorted(set(files))))
PY
}

check_install_ownership() {
    local platform="$1"
    local planned_files
    planned_files=$(plan_install_files "$platform")
    python3 "$STATE_TOOL" check-ownership --db "$ADPM_DB" \
        --package "$PACKAGE_NAME" --files "$planned_files"
}

# ── Package DB ────────────────────────────────────────────────
db_record_install() {
    local name="$1" version="$2" prefix="$3" files_json="$4"
    local record="$TEMP_DIR/install-record.json"
    python3 - "$TEMP_DIR/META.json" "$name" "$version" "$prefix" "$files_json" \
        "$CURRENT_ARCHIVE" "$CURRENT_ARCHIVE_SHA256" > "$record" <<'PY'
import datetime, json, pathlib, sys
meta_path, name, version, prefix, files, archive, checksum = sys.argv[1:]
metadata = json.load(open(meta_path))
record = {
    "name": name, "version": version, "prefix": prefix,
    "installed_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "files": json.loads(files), "archive_path": archive, "archive_sha256": checksum,
    "source_format": metadata.get("source_format", "adpm"),
    "provenance": metadata.get("provenance", {}),
}
print(json.dumps(record))
PY
    python3 "$STATE_TOOL" install --db "$ADPM_DB" --record - --action "$INSTALL_ACTION" < "$record"
}

db_record_remove() {
    local name="$1"
    python3 "$STATE_TOOL" remove --db "$ADPM_DB" --package "$name"
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

create_rollback_snapshot() {
    local name="$1"
    local record="$ADPM_DB/installed/${name}.json"
    local snapshot="$ADPM_DB/rollback/$name"
    [ -f "$record" ] || return 0
    python3 - "$record" "$snapshot" <<'PY'
import json, pathlib, shutil, sys
record_path, snapshot = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
if snapshot.exists():
    shutil.rmtree(snapshot)
(snapshot / "files").mkdir(parents=True)
record = json.load(open(record_path))
manifest = []
for index, target in enumerate(record.get("files", [])):
    source = pathlib.Path(target)
    if source.is_file():
        backup = snapshot / "files" / str(index)
        shutil.copy2(source, backup)
        manifest.append({"target": str(source), "backup": str(backup)})
json.dump(record, open(snapshot / "record.json", "w"), indent=2)
json.dump(manifest, open(snapshot / "manifest.json", "w"), indent=2)
PY
}

rollback_package() {
    local name="$1"
    PACKAGE_NAME="$name"
    local snapshot="$ADPM_DB/rollback/$name"
    [ -f "$snapshot/record.json" ] || { log_error "No rollback snapshot for '$name'"; exit 1; }
    resolve_state_tool
    local restored_record="$TEMP_DIR/rollback-record.json"
    local rollback_files
    rollback_files=$(python3 - "$snapshot/record.json" <<'PY'
import json, sys
print(json.dumps(json.load(open(sys.argv[1])).get("files", [])))
PY
)
    python3 "$STATE_TOOL" check-ownership --db "$ADPM_DB" --package "$name" --files "$rollback_files"
    python3 - "$name" "$ADPM_DB" "$snapshot" "$restored_record" <<'PY'
import json, pathlib, shutil, sys
name, db, snapshot, restored = sys.argv[1], pathlib.Path(sys.argv[2]), pathlib.Path(sys.argv[3]), pathlib.Path(sys.argv[4])
current = db / "installed" / (name + ".json")
if current.exists():
    for target in json.load(open(current)).get("files", []):
        path = pathlib.Path(target)
        if path.is_file() or path.is_symlink():
            path.unlink()
for item in json.load(open(snapshot / "manifest.json")):
    target = pathlib.Path(item["target"])
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(item["backup"], target)
shutil.copy2(snapshot / "record.json", restored)
PY
    python3 "$STATE_TOOL" install --db "$ADPM_DB" --record - --action rollback < "$restored_record"
    log_success "Rolled '$name' back to the previous installed version"
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

verify_checksum() {
    local archive_file="$1"
    local required="$2"
    local checksum_file="${archive_file}.sha256"
    if [ ! -f "$checksum_file" ]; then
        [ "$required" = "1" ] && { log_error "Checksum file missing ($checksum_file)"; exit 1; }
        return 0
    fi
    local expected actual
    expected=$(awk 'NR == 1 {print $1}' "$checksum_file")
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$archive_file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$archive_file" | awk '{print $1}')
    else
        log_error "No SHA-256 utility available"
        exit 1
    fi
    [ "$expected" = "$actual" ] || { log_error "SHA-256 checksum verification FAILED"; exit 1; }
    log_success "SHA-256 checksum verified"
}

# ── Install ───────────────────────────────────────────────────
install_package() {
    local platform="$1"
    local files_json="[]"

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
        done
    fi

    # Converted native packages retain non-bin/lib files under payload/. Map
    # usr/ into the selected prefix so user and system installs stay isolated.
    local payload_dir="$TEMP_DIR/payload/$platform"
    if [ -d "$payload_dir" ] && command -v python3 >/dev/null 2>&1; then
        log_info "Installing preserved package payload → $INSTALL_PREFIX"
        python3 - "$payload_dir" "$INSTALL_PREFIX" <<'PY'
import os, pathlib, shutil, sys
source, prefix = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
def mapped(relative):
    parts = relative.parts
    if parts[:2] == ("usr", "local"):
        relative = pathlib.Path(*parts[2:])
    elif parts[:1] == ("usr",):
        relative = pathlib.Path(*parts[1:])
    return prefix / relative

for item in source.rglob("*"):
    if item.is_dir():
        continue
    relative = item.relative_to(source)
    target = mapped(relative)
    target.parent.mkdir(parents=True, exist_ok=True)
    if item.is_symlink():
        if target.exists() or target.is_symlink():
            target.unlink()
        source_target = (item.parent / os.readlink(item)).resolve()
        source_relative = source_target.relative_to(source.resolve())
        target.symlink_to(os.path.relpath(mapped(source_relative), target.parent))
    else:
        shutil.copyfile(item, target)
        target.chmod(item.stat().st_mode & 0o777)
PY
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

    # Reuse the safely JSON-encoded preflight plan for the ownership record.
    # At this point every copy operation has succeeded under `set -e`.
    files_json=$(plan_install_files "$platform")

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
    resolve_state_tool
    if command -v python3 >/dev/null 2>&1; then
        files_list=$(echo "$info" | python3 -c '
import json, pathlib, sys
record = json.loads(sys.stdin.read())
owners_path = pathlib.Path(sys.argv[1]) / "owners.json"
owners = json.load(open(owners_path)).get("files", {}) if owners_path.exists() else {}
name = sys.argv[2]
print("\n".join(path for path in record.get("files", [])
                if path not in owners or owners[path].get("package") == name))
' "$ADPM_DB" "$name" 2>/dev/null || true)
    fi

    if [ -n "$files_list" ]; then
        log_info "Removing tracked files..."
        while IFS= read -r f; do
            if [ -n "$f" ] && { [ -f "$f" ] || [ -L "$f" ]; }; then
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
    CURRENT_ARCHIVE="$archive"
    read_metadata
    resolve_state_tool
    check_dependencies || { log_error "Dependency resolution failed"; exit 1; }
    local platform
    platform=$(detect_platform)
    [ "$platform" = "unknown" ] && { log_error "Unsupported platform"; exit 1; }
    check_platform "$platform" || { log_error "Package is incompatible with this platform"; exit 1; }
    check_install_ownership "$platform" || { log_error "Package file ownership conflict"; exit 1; }

    local existing
    existing=$(db_get_installed "$PACKAGE_NAME")
    if [ -n "$existing" ]; then
        local old_version
        old_version=$(echo "$existing" | grep '"version"' | cut -d'"' -f4)
        log_info "Upgrading $PACKAGE_NAME: v$old_version → v$PACKAGE_VERSION"
        create_rollback_snapshot "$PACKAGE_NAME"
        uninstall_package "$PACKAGE_NAME"
        INSTALL_ACTION="upgrade"
    else
        log_info "No existing installation found — performing fresh install"
    fi

    # Re-extract (uninstall_package cleanup already happened)
    # We already extracted above, so just install
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
        --rollback)
            [ -z "${2:-}" ] && { log_error "Usage: $0 --rollback PACKAGE"; exit 1; }
            mode="rollback"
            ;;
        --help|-h)
            echo "ADPM Installer v$ADPM_VERSION"
            echo ""
            echo "Usage:"
            echo "  $0 package.adpm               # Install package"
            echo "  $0 --uninstall PACKAGE        # Uninstall package"
            echo "  $0 --upgrade package.adpm     # Upgrade package"
            echo "  $0 --rollback PACKAGE         # Restore version saved by last upgrade"
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
    echo "  ADPM - After Dark Systems Package Manager v$ADPM_VERSION"
    echo "  Homage to Todd Bennett III, unixeng"
    echo "============================================"
    echo ""

    case "$mode" in
        uninstall)
            uninstall_package "$2"
            exit 0
            ;;
        upgrade)
            if [ "$verify" -eq 1 ]; then
                verify_checksum "$2" "$verify_req"
                verify_signature "$2" "$verify_req"
            fi
            upgrade_package "$2"
            ;;
        rollback)
            rollback_package "$2"
            ;;
        install)
            local target_arch=""
            if is_self_extracting; then
                log_info "Self-extracting installer"
                CURRENT_ARCHIVE="$0"
            else
                [ -z "$arg1" ] && { log_error "Usage: $0 <package.adpm>"; exit 1; }
                target_arch="$arg1"
                CURRENT_ARCHIVE="$target_arch"
            fi
            
            if [ -n "$target_arch" ] && [ "$verify" -eq 1 ]; then
                verify_checksum "$target_arch" "$verify_req"
                verify_signature "$target_arch" "$verify_req"
            fi
            
            extract_archive "$target_arch" "$TEMP_DIR"

            read_metadata
            resolve_state_tool

            check_dependencies || { log_error "Dependency resolution failed"; exit 1; }

            local platform
            platform=$(detect_platform)
            if [ "$platform" = "unknown" ]; then
                log_error "Unsupported platform: $(uname -s)/$(uname -m)"
                exit 1
            fi

            log_info "Platform: $platform"
            log_info "Prefix:   $INSTALL_PREFIX"
            echo ""

            check_platform "$platform" || { log_error "Package is incompatible with this platform"; exit 1; }
            check_install_ownership "$platform" || { log_error "Package file ownership conflict"; exit 1; }

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
    echo "  adpm install --uninstall $PACKAGE_NAME"
    echo ""
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi

exit 0

__ADPM_ARCHIVE_BELOW__
