#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════
#   ADPM — Multi-Format Package Builder
#   Homage to Todd Bennett III, unixeng
#
#   Builds .tar.gz, .zip, .deb, .rpm, and .adpm packages
#   from a pre-compiled binary directory.
#
#   Usage:
#     ./builder/build-formats.sh \
#       --name ss \
#       --version 0.3.1 \
#       --binary ss \
#       --bins-dir /path/to/compiled-binaries \
#       --output dist/
#
#   Expects binaries named:  ss_0.3.1_darwin-arm64
#                             ss_0.3.1_linux-x86_64
#                             ss_0.3.1_windows-x86_64.exe  etc.
#
#   Requires:
#     fpm   — gem install fpm      (.deb / .rpm)
#     bzip2 — standard Unix        (.adpm)
#     cpio  — standard Unix        (.adpm)
#     zip   — standard Unix        (Windows .zip)
# ════════════════════════════════════════════════════════════════
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

ADPM_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die()  { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }
ok()   { echo -e "  ${GREEN}✓${NC} $*"; }
warn() { echo -e "  ${YELLOW}⚠  $*${NC}"; }

# ── Args ──────────────────────────────────────────────────────
PKG_NAME=""
VERSION=""
BINARY_NAME=""
BINS_DIR=""
OUTPUT_DIR="dist"
MAINTAINER="AfterDark Systems <support@afterdarksys.com>"
DESCRIPTION="AfterDark package"
HOMEPAGE="https://afterdarksys.com"
LICENSE="Proprietary"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --name)        PKG_NAME="$2";     shift 2 ;;
        --version)     VERSION="$2";      shift 2 ;;
        --binary)      BINARY_NAME="$2";  shift 2 ;;
        --bins-dir)    BINS_DIR="$2";     shift 2 ;;
        --output)      OUTPUT_DIR="$2";   shift 2 ;;
        --maintainer)  MAINTAINER="$2";   shift 2 ;;
        --description) DESCRIPTION="$2";  shift 2 ;;
        --homepage)    HOMEPAGE="$2";     shift 2 ;;
        --license)     LICENSE="$2";      shift 2 ;;
        *) die "Unknown argument: $1" ;;
    esac
done

[[ -z "$PKG_NAME" ]]    && die "--name required"
[[ -z "$VERSION" ]]     && die "--version required"
[[ -z "$BINARY_NAME" ]] && die "--binary required"
[[ -z "$BINS_DIR" ]]    && die "--bins-dir required"
[[ ! -d "$BINS_DIR" ]]  && die "bins-dir not found: $BINS_DIR"

VERSION="${VERSION#v}"
mkdir -p "$OUTPUT_DIR"

# ── Banner ────────────────────────────────────────────────────
echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}   📦 ADPM Multi-Format Package Builder${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  Package:  ${YELLOW}${PKG_NAME} v${VERSION}${NC}"
echo -e "  Binary:   ${YELLOW}${BINARY_NAME}${NC}"
echo -e "  Source:   ${YELLOW}${BINS_DIR}${NC}"
echo -e "  Output:   ${YELLOW}${OUTPUT_DIR}${NC}"
echo ""

# Platform map: label → (goos, goarch, deb_arch, rpm_arch, exe_ext)
declare -A DEB_ARCH=( [linux-x86_64]="amd64"   [linux-aarch64]="arm64"  )
declare -A RPM_ARCH=( [linux-x86_64]="x86_64"  [linux-aarch64]="aarch64")

# ── .tar.gz / .zip ────────────────────────────────────────────
echo -e "${CYAN}${BOLD}[1/3] Archives (.tar.gz / .zip)${NC}"

for BIN_FILE in "$BINS_DIR"/${PKG_NAME}_${VERSION}_*; do
    [[ -f "$BIN_FILE" ]] || continue
    BASENAME="$(basename "$BIN_FILE")"

    # Extract platform from filename: name_version_platform[.exe]
    PLATFORM="${BASENAME#${PKG_NAME}_${VERSION}_}"
    PLATFORM="${PLATFORM%.exe}"

    if [[ "$PLATFORM" == windows-* ]]; then
        ARCHIVE="$OUTPUT_DIR/${PKG_NAME}_${VERSION}_${PLATFORM}.zip"
        echo -ne "  ${YELLOW}.zip${NC} ${PLATFORM}... "
        STAGE="$(mktemp -d)"
        cp "$BIN_FILE" "$STAGE/${BINARY_NAME}.exe"
        (cd "$STAGE" && zip -q "$ARCHIVE" "${BINARY_NAME}.exe")
        rm -rf "$STAGE"
        echo -e "${GREEN}✓${NC} $(du -sh "$ARCHIVE" | cut -f1)"
    else
        ARCHIVE="$OUTPUT_DIR/${PKG_NAME}_${VERSION}_${PLATFORM}.tar.gz"
        echo -ne "  ${YELLOW}.tar.gz${NC} ${PLATFORM}... "
        STAGE="$(mktemp -d)"
        cp "$BIN_FILE" "$STAGE/$BINARY_NAME"
        chmod 755 "$STAGE/$BINARY_NAME"
        printf '%s v%s\n%s\n' "$PKG_NAME" "$VERSION" "$HOMEPAGE" > "$STAGE/README"
        (cd "$STAGE" && tar czf "$ARCHIVE" "$BINARY_NAME" README)
        rm -rf "$STAGE"
        echo -e "${GREEN}✓${NC} $(du -sh "$ARCHIVE" | cut -f1)"
    fi
done

# ── .deb / .rpm ───────────────────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}[2/3] Linux Packages (.deb / .rpm)${NC}"

if ! command -v fpm &>/dev/null; then
    warn "fpm not found — skipping .deb/.rpm"
    warn "  Install: gem install fpm"
else
    for PLAT in linux-x86_64 linux-aarch64; do
        BIN_FILE="$BINS_DIR/${PKG_NAME}_${VERSION}_${PLAT}"
        [[ -f "$BIN_FILE" ]] || continue

        STAGE="$(mktemp -d)"
        mkdir -p "$STAGE/usr/local/bin" "$STAGE/usr/share/doc/$PKG_NAME"
        cp "$BIN_FILE" "$STAGE/usr/local/bin/$BINARY_NAME"
        chmod 755 "$STAGE/usr/local/bin/$BINARY_NAME"
        printf '%s v%s\n%s\n' "$PKG_NAME" "$VERSION" "$HOMEPAGE" > "$STAGE/usr/share/doc/$PKG_NAME/README"

        # .deb
        DA="${DEB_ARCH[$PLAT]}"
        DEB_OUT="$OUTPUT_DIR/${PKG_NAME}_${VERSION}_${DA}.deb"
        echo -ne "  ${YELLOW}.deb${NC} ${PLAT}... "
        fpm --input-type dir --output-type deb \
            --name "$PKG_NAME" --version "$VERSION" \
            --architecture "$DA" \
            --maintainer "$MAINTAINER" \
            --description "$DESCRIPTION" \
            --url "$HOMEPAGE" --license "$LICENSE" \
            --deb-no-default-config-files \
            --package "$DEB_OUT" --chdir "$STAGE" \
            . 2>&1 | grep -Ev "^$" | sed 's/^/    /' || true
        echo -e "${GREEN}✓${NC} $(du -sh "$DEB_OUT" | cut -f1)"

        # .rpm
        RA="${RPM_ARCH[$PLAT]}"
        RPM_OUT="$OUTPUT_DIR/${PKG_NAME}_${VERSION}_${RA}.rpm"
        echo -ne "  ${YELLOW}.rpm${NC} ${PLAT}... "
        fpm --input-type dir --output-type rpm \
            --name "$PKG_NAME" --version "$VERSION" \
            --architecture "$RA" \
            --maintainer "$MAINTAINER" \
            --description "$DESCRIPTION" \
            --url "$HOMEPAGE" --license "$LICENSE" \
            --package "$RPM_OUT" --chdir "$STAGE" \
            . 2>&1 | grep -Ev "^$" | sed 's/^/    /' || true
        echo -e "${GREEN}✓${NC} $(du -sh "$RPM_OUT" | cut -f1)"

        rm -rf "$STAGE"
    done
fi

# ── .adpm + self-extracting installers ───────────────────────
echo ""
echo -e "${CYAN}${BOLD}[3/3] ADPM Packages + Self-Extracting Installers${NC}"

ADPM_BUILDER="$ADPM_ROOT/builder/adpm-build.py"
MAKE_SELFEX="$ADPM_ROOT/builder/make-self-extracting.sh"

for PLAT in darwin-arm64 darwin-x86_64 linux-x86_64 linux-aarch64; do
    BIN_FILE="$BINS_DIR/${PKG_NAME}_${VERSION}_${PLAT}"
    [[ -f "$BIN_FILE" ]] || continue

    echo -ne "  ${YELLOW}.adpm${NC} ${PLAT}... "
    python3 "$ADPM_BUILDER" \
        --name "$PKG_NAME" \
        --version "$VERSION" \
        --platform "$PLAT" \
        --binaries "$BIN_FILE" \
        --output "$OUTPUT_DIR" 2>&1 | grep -v "^$" | sed 's/^/    /' || true

    ADPM_FILE="$OUTPUT_DIR/${PKG_NAME}-${VERSION}.adpm"
    if [[ -f "$ADPM_FILE" ]]; then
        ADPM_PLAT="$OUTPUT_DIR/${PKG_NAME}_${VERSION}_${PLAT}.adpm"
        mv "$ADPM_FILE" "$ADPM_PLAT"
        INSTALLER="$OUTPUT_DIR/installer-${PLAT}"
        bash "$MAKE_SELFEX" "$ADPM_PLAT" "$INSTALLER" 2>&1 | grep -v "^$" | sed 's/^/    /' || true
        echo -e "${GREEN}✓${NC} ${ADPM_PLAT##*/} + installer"
    else
        warn "ADPM builder produced no file for $PLAT"
    fi
done

# ── Summary ───────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}   ✅  Packages built → ${OUTPUT_DIR}/${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo ""
ls -lh "$OUTPUT_DIR" | tail -n +2 | awk '{print "  " $NF " (" $5 ")"}' || true
echo ""
