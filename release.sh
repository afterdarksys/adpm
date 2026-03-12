#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════
#   ADPM — AfterDark Package Manager Release Script
#   Homage to Todd Bennett III, unixeng
#
#   Usage:
#     ./release.sh --pkg packages/ss.pkg.json --source ~/dev/myproject VERSION
#     ./release.sh --pkg packages/ss.pkg.json --source ~/dev/myproject  # prompts
#
#   Builds ALL distribution formats from a single .pkg.json definition:
#     .tar.gz        GitHub Releases + Homebrew source
#     .zip           Windows GitHub Releases
#     .deb           Debian / Ubuntu
#     .rpm           RHEL / Fedora / CentOS
#     .adpm          AfterDark Package Manager (self-extracting)
#     installer-*    ADPM self-extracting installers per platform
#
#   Publishes to:
#     GitHub Releases  (all artifacts)
#     Homebrew tap     (formula auto-generated with correct SHA256s)
#
#   End-user install options:
#     brew install <owner>/tap/<formula>          (macOS, Homebrew)
#     curl -sSL https://yoursite/install | sh     (curl installer)
#     sudo dpkg -i name_version_amd64.deb         (Debian/Ubuntu)
#     sudo rpm -i name_version_x86_64.rpm         (RHEL/Fedora)
#     ./adpm-install.sh name-version.adpm         (ADPM)
#
#   Requires:
#     go (>=1.21)             — brew install go
#     fpm                     — gem install fpm
#     gh                      — brew install gh
#     python3                 — for adpm-build.py
#     bzip2, cpio             — standard Unix tools
#     export GITHUB_TOKEN=ghp_...
#     export HOMEBREW_TAP_TOKEN=ghp_...   (warn if missing, skips brew)
# ════════════════════════════════════════════════════════════════
set -euo pipefail

# ── Colors ────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m'

ADPM_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Parse args ────────────────────────────────────────────────
PKG_FILE=""
SOURCE_DIR=""
VERSION=""
DRY_RUN=0
SKIP_GITHUB=0
SKIP_BREW=0
SKIP_PACKAGES=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --pkg)      PKG_FILE="$2";    shift 2 ;;
        --source)   SOURCE_DIR="$2";  shift 2 ;;
        --dry-run)  DRY_RUN=1;        shift ;;
        --no-github) SKIP_GITHUB=1;   shift ;;
        --no-brew)   SKIP_BREW=1;     shift ;;
        --no-packages) SKIP_PACKAGES=1; shift ;;
        -*)         echo -e "${RED}Unknown flag: $1${NC}"; exit 1 ;;
        *)          VERSION="$1";     shift ;;
    esac
done

# ── Helpers ───────────────────────────────────────────────────
die() { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }
ok()  { echo -e "  ${GREEN}✓${NC} $*"; }
warn(){ echo -e "  ${YELLOW}⚠  $*${NC}"; }
step(){ echo -e "\n${CYAN}${BOLD}$*${NC}"; }

# Read value from pkg.json using python3 (always available)
pkg() {
    local keypath="$1"
    python3 - "$PKG_FILE" "$keypath" <<'PYEOF'
import json, sys

def get(d, path):
    for k in path.split("."):
        if isinstance(d, dict) and k in d:
            d = d[k]
        else:
            return ""
    return str(d) if not isinstance(d, (dict, list)) else ""

with open(sys.argv[1]) as f:
    data = json.load(f)
print(get(data, sys.argv[2]))
PYEOF
}

pkg_array() {
    local keypath="$1"
    python3 - "$PKG_FILE" "$keypath" <<'PYEOF'
import json, sys

def get(d, path):
    for k in path.split("."):
        if isinstance(d, dict) and k in d:
            d = d[k]
        else:
            return []
    return d if isinstance(d, list) else []

with open(sys.argv[1]) as f:
    data = json.load(f)
for item in get(data, sys.argv[2]):
    print(item)
PYEOF
}

# ── Banner ────────────────────────────────────────────────────
echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}   📦 ADPM — AfterDark Package Manager Release Script${NC}"
echo -e "${BLUE}   Homage to Todd Bennett III, unixeng${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""
[[ "$DRY_RUN" == "1" ]] && echo -e "  ${YELLOW}${BOLD}DRY RUN MODE — no packages built, no publishing${NC}\n"

# ── Step 1: Package Definition ────────────────────────────────
step "[1/7] Package Definition"

[[ -z "$PKG_FILE" ]] && die "Usage: $0 --pkg packages/NAME.pkg.json --source /path/to/source [VERSION]"
[[ ! -f "$PKG_FILE" ]] && die "Package file not found: $PKG_FILE"

PKG_NAME=$(pkg "name")
PKG_DISPLAY=$(pkg "display_name")
PKG_DESC=$(pkg "description")
PKG_LICENSE=$(pkg "license")
PKG_MAINTAINER=$(pkg "maintainer")
PKG_HOMEPAGE=$(pkg "homepage")
GH_OWNER=$(pkg "github.owner")
GH_REPO=$(pkg "github.repo")
BREW_TAP_OWNER=$(pkg "homebrew.tap_owner")
BREW_TAP_REPO=$(pkg "homebrew.tap_repo")
BREW_FORMULA=$(pkg "homebrew.formula")
BUILD_TYPE=$(pkg "build.type")
BUILD_MAIN=$(pkg "build.main")
BUILD_BINARY=$(pkg "build.binary")
BUILD_MODULE=$(pkg "build.module")
BUILD_VERSION_VAR=$(pkg "build.version_var")
BUILD_COMMIT_VAR=$(pkg "build.commit_var")
BUILD_DATE_VAR=$(pkg "build.date_var")

[[ -z "$PKG_NAME" ]]    && die "pkg.json missing: name"
[[ -z "$GH_OWNER" ]]    && die "pkg.json missing: github.owner"
[[ -z "$GH_REPO" ]]     && die "pkg.json missing: github.repo"
[[ -z "$BUILD_TYPE" ]]  && die "pkg.json missing: build.type"

ok "Package: ${BOLD}$PKG_DISPLAY${NC} (${PKG_NAME})"
ok "Build:   ${BUILD_TYPE} → ${BUILD_BINARY}"
ok "GitHub:  ${GH_OWNER}/${GH_REPO}"
[[ -n "$BREW_TAP_OWNER" ]] && ok "Homebrew: ${BREW_TAP_OWNER}/${BREW_TAP_REPO} → ${BREW_FORMULA}"

# ── Step 2: Version ───────────────────────────────────────────
step "[2/7] Version"

if [[ -z "$VERSION" ]]; then
    if [[ -n "$SOURCE_DIR" ]] && command -v git &>/dev/null; then
        LATEST_TAG=$(git -C "$SOURCE_DIR" describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
        LATEST_VER="${LATEST_TAG#v}"
        IFS='.' read -r MAJOR MINOR PATCH <<< "$LATEST_VER"
        SUGGESTED="$MAJOR.$MINOR.$((PATCH + 1))"
        echo -e "  Latest tag: ${YELLOW}$LATEST_TAG${NC}"
    else
        SUGGESTED="0.1.0"
    fi
    echo -ne "  New version [${GREEN}$SUGGESTED${NC}]: "
    read -r INPUT
    VERSION="${INPUT:-$SUGGESTED}"
fi
VERSION="${VERSION#v}"
TAG="v$VERSION"

ok "Version: ${BOLD}$TAG${NC}"

# ── Step 3: Pre-flight ────────────────────────────────────────
step "[3/7] Pre-flight"

# Source directory
if [[ -z "$SOURCE_DIR" ]] && [[ "$BUILD_TYPE" == "go" ]]; then
    die "--source /path/to/go-module is required for Go packages"
fi
if [[ -n "$SOURCE_DIR" ]]; then
    SOURCE_DIR="$(cd "$SOURCE_DIR" && pwd)"
    [[ ! -f "$SOURCE_DIR/go.mod" ]] && die "No go.mod in --source directory: $SOURCE_DIR"
    ok "Source: $SOURCE_DIR"
fi

# Go
if [[ "$BUILD_TYPE" == "go" ]]; then
    command -v go &>/dev/null || die "go not found. Install: brew install go"
    ok "Go: $(go version | awk '{print $3}')"
fi

# fpm
if [[ "$SKIP_PACKAGES" == "0" ]]; then
    if ! command -v fpm &>/dev/null; then
        warn "fpm not found — .deb/.rpm will be skipped"
        warn "  Install: gem install fpm"
        HAS_FPM=0
    else
        ok "fpm: $(fpm --version 2>&1 | head -1)"
        HAS_FPM=1
    fi
fi

# gh CLI
if [[ "$SKIP_GITHUB" == "0" ]]; then
    command -v gh &>/dev/null || die "gh CLI not found. Install: brew install gh"
    ok "gh: $(gh --version | head -1)"
fi

# bzip2 + cpio (for ADPM)
command -v bzip2 &>/dev/null || die "bzip2 not found"
command -v cpio  &>/dev/null || die "cpio not found"
ok "bzip2 + cpio: available"

# GITHUB_TOKEN
if [[ "$SKIP_GITHUB" == "0" ]]; then
    [[ -z "${GITHUB_TOKEN:-}" ]] && die "GITHUB_TOKEN not set. export GITHUB_TOKEN=ghp_..."
    ok "GITHUB_TOKEN: set"
fi

# HOMEBREW_TAP_TOKEN
if [[ -n "$BREW_TAP_OWNER" ]] && [[ "$SKIP_BREW" == "0" ]]; then
    if [[ -z "${HOMEBREW_TAP_TOKEN:-}" ]]; then
        warn "HOMEBREW_TAP_TOKEN not set — Homebrew tap will be skipped"
        SKIP_BREW=1
    else
        ok "HOMEBREW_TAP_TOKEN: set"
    fi
fi

# Git tag check
if [[ -n "$SOURCE_DIR" ]] && [[ "$SKIP_GITHUB" == "0" ]]; then
    if git -C "$SOURCE_DIR" rev-parse "$TAG" &>/dev/null; then
        die "Tag $TAG already exists. Delete it first: git tag -d $TAG && git push origin :$TAG"
    fi
    ok "Tag $TAG: available"
fi

# Output dirs
DIST="$ADPM_ROOT/dist/$PKG_NAME-$VERSION"
mkdir -p "$DIST"
ok "Output dir: $DIST"

# ── Step 4: Build Binaries ────────────────────────────────────
step "[4/7] Build Binaries"

declare -A BINS  # platform → binary path

if [[ "$BUILD_TYPE" == "go" ]]; then
    COMMIT=$(git -C "$SOURCE_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")
    BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    LDFLAGS="-s -w"
    [[ -n "$BUILD_VERSION_VAR" ]] && LDFLAGS="$LDFLAGS -X ${BUILD_VERSION_VAR}=${VERSION}"
    [[ -n "$BUILD_COMMIT_VAR" ]]  && LDFLAGS="$LDFLAGS -X ${BUILD_COMMIT_VAR}=${COMMIT}"
    [[ -n "$BUILD_DATE_VAR" ]]    && LDFLAGS="$LDFLAGS -X ${BUILD_DATE_VAR}=${BUILD_DATE}"

    # Platform map: adpm-name → GOOS:GOARCH:ext
    declare -A PLAT_MAP=(
        ["darwin-arm64"]="darwin:arm64:"
        ["darwin-x86_64"]="darwin:amd64:"
        ["linux-x86_64"]="linux:amd64:"
        ["linux-aarch64"]="linux:arm64:"
        ["windows-x86_64"]="windows:amd64:.exe"
    )

    while IFS= read -r plat; do
        [[ -z "$plat" ]] && continue
        if [[ -z "${PLAT_MAP[$plat]:-}" ]]; then
            warn "Unknown platform in pkg.json: $plat — skipping"
            continue
        fi

        IFS=':' read -r GOOS GOARCH EXT <<< "${PLAT_MAP[$plat]}"
        BIN_PATH="$DIST/${BUILD_BINARY}_${VERSION}_${plat}${EXT}"

        echo -ne "  Compiling ${YELLOW}${plat}${NC}... "

        if [[ "$DRY_RUN" == "0" ]]; then
            GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build \
                -ldflags "$LDFLAGS" \
                -o "$BIN_PATH" \
                "$SOURCE_DIR/$BUILD_MAIN" 2>&1 | sed 's/^/    /'
            echo -e "${GREEN}✓${NC} ($(du -sh "$BIN_PATH" | cut -f1))"
        else
            echo -e "${YELLOW}(dry-run)${NC}"
        fi

        BINS[$plat]="$BIN_PATH"

    done < <(pkg_array "platforms")
fi

# ── Step 5: Build Packages ────────────────────────────────────
step "[5/7] Build Packages"

declare -A TARBALLS  # platform → .tar.gz path

# .tar.gz / .zip per platform
echo -e "  ${BOLD}Archives${NC}"
for plat in "${!BINS[@]}"; do
    BIN="${BINS[$plat]}"
    BINNAME="$(basename "$BIN")"

    if [[ "$plat" == windows-* ]]; then
        ARCHIVE="$DIST/${PKG_NAME}_${VERSION}_${plat}.zip"
        echo -ne "    ${YELLOW}.zip${NC} $plat... "
        if [[ "$DRY_RUN" == "0" ]]; then
            STAGE="$(mktemp -d)"
            cp "$BIN" "$STAGE/${BUILD_BINARY}.exe"
            (cd "$STAGE" && zip -q "$ARCHIVE" "${BUILD_BINARY}.exe")
            rm -rf "$STAGE"
            echo -e "${GREEN}✓${NC}"
        else
            echo -e "${YELLOW}(dry-run)${NC}"
        fi
        TARBALLS[$plat]="$ARCHIVE"
    else
        ARCHIVE="$DIST/${PKG_NAME}_${VERSION}_${plat}.tar.gz"
        echo -ne "    ${YELLOW}.tar.gz${NC} $plat... "
        if [[ "$DRY_RUN" == "0" ]]; then
            STAGE="$(mktemp -d)"
            cp "$BIN" "$STAGE/$BUILD_BINARY"
            chmod 755 "$STAGE/$BUILD_BINARY"
            cat > "$STAGE/README" <<DOCEOF
${PKG_DISPLAY} v${VERSION}
${PKG_HOMEPAGE}

Usage: ${BUILD_BINARY} --help
Docs:  ${PKG_HOMEPAGE}/docs/cli
DOCEOF
            (cd "$STAGE" && tar czf "$ARCHIVE" "$BUILD_BINARY" README)
            rm -rf "$STAGE"
            echo -e "${GREEN}✓${NC}"
        else
            echo -e "${YELLOW}(dry-run)${NC}"
        fi
        TARBALLS[$plat]="$ARCHIVE"
    fi
done

if [[ "$SKIP_PACKAGES" == "0" ]]; then

    # .deb + .rpm (Linux only, via fpm)
    if [[ "$HAS_FPM" == "1" ]]; then
        echo ""
        echo -e "  ${BOLD}Linux packages (.deb / .rpm)${NC}"

        for plat in linux-x86_64 linux-aarch64; do
            [[ -z "${BINS[$plat]:-}" ]] && continue

            BIN="${BINS[$plat]}"
            DEB_ARCH="$([[ "$plat" == *aarch64 ]] && echo arm64 || echo amd64)"
            RPM_ARCH="$([[ "$plat" == *aarch64 ]] && echo aarch64 || echo x86_64)"

            # Stage tree for fpm
            STAGE="$(mktemp -d)"
            mkdir -p "$STAGE/usr/local/bin" "$STAGE/usr/share/doc/$PKG_NAME"
            cp "$BIN" "$STAGE/usr/local/bin/$BUILD_BINARY"
            chmod 755 "$STAGE/usr/local/bin/$BUILD_BINARY"
            cat > "$STAGE/usr/share/doc/$PKG_NAME/README" <<DOCEOF
${PKG_DISPLAY} v${VERSION}
${PKG_HOMEPAGE}

Usage: ${BUILD_BINARY} --help
DOCEOF

            # .deb
            DEB_OUT="$DIST/${PKG_NAME}_${VERSION}_${DEB_ARCH}.deb"
            echo -ne "    ${YELLOW}.deb${NC} $plat... "
            if [[ "$DRY_RUN" == "0" ]]; then
                fpm \
                    --input-type dir \
                    --output-type deb \
                    --name "$PKG_NAME" \
                    --version "$VERSION" \
                    --architecture "$DEB_ARCH" \
                    --maintainer "$PKG_MAINTAINER" \
                    --description "$PKG_DESC" \
                    --url "$PKG_HOMEPAGE" \
                    --license "$PKG_LICENSE" \
                    --deb-no-default-config-files \
                    --package "$DEB_OUT" \
                    --chdir "$STAGE" \
                    . 2>&1 | grep -Ev "^$|Created package" | sed 's/^/      /' || true
                echo -e "${GREEN}✓${NC} ${DEB_OUT##*/}"
            else
                echo -e "${YELLOW}(dry-run)${NC}"
            fi

            # .rpm
            RPM_OUT="$DIST/${PKG_NAME}_${VERSION}_${RPM_ARCH}.rpm"
            echo -ne "    ${YELLOW}.rpm${NC} $plat... "
            if [[ "$DRY_RUN" == "0" ]]; then
                fpm \
                    --input-type dir \
                    --output-type rpm \
                    --name "$PKG_NAME" \
                    --version "$VERSION" \
                    --architecture "$RPM_ARCH" \
                    --maintainer "$PKG_MAINTAINER" \
                    --description "$PKG_DESC" \
                    --url "$PKG_HOMEPAGE" \
                    --license "$PKG_LICENSE" \
                    --package "$RPM_OUT" \
                    --chdir "$STAGE" \
                    . 2>&1 | grep -Ev "^$|Created package" | sed 's/^/      /' || true
                echo -e "${GREEN}✓${NC} ${RPM_OUT##*/}"
            else
                echo -e "${YELLOW}(dry-run)${NC}"
            fi

            rm -rf "$STAGE"
        done
    fi

    # .adpm + self-extracting installer per platform
    echo ""
    echo -e "  ${BOLD}ADPM packages (.adpm + self-extracting installers)${NC}"

    ADPM_BUILDER="$ADPM_ROOT/builder/adpm-build.py"
    MAKE_SELFEX="$ADPM_ROOT/builder/make-self-extracting.sh"

    for plat in "${!BINS[@]}"; do
        [[ "$plat" == windows-* ]] && continue  # ADPM targets Unix
        BIN="${BINS[$plat]}"

        echo -ne "    ${YELLOW}.adpm${NC} $plat... "
        if [[ "$DRY_RUN" == "0" ]]; then
            python3 "$ADPM_BUILDER" \
                --name "$PKG_NAME" \
                --version "$VERSION" \
                --platform "$plat" \
                --binaries "$BIN" \
                --output "$DIST" 2>&1 | grep -v "^$" | sed 's/^/      /' || true

            ADPM_FILE="$DIST/${PKG_NAME}-${VERSION}.adpm"
            INSTALLER_OUT="$DIST/installer-${plat}"

            if [[ -f "$ADPM_FILE" ]]; then
                # Rename per-platform (builder always names it name-version.adpm)
                ADPM_PLAT="$DIST/${PKG_NAME}_${VERSION}_${plat}.adpm"
                mv "$ADPM_FILE" "$ADPM_PLAT"

                # Self-extracting installer
                bash "$MAKE_SELFEX" "$ADPM_PLAT" "$INSTALLER_OUT" 2>&1 | grep -v "^$" | sed 's/^/      /' || true
                echo -e "${GREEN}✓${NC} ${ADPM_PLAT##*/} + installer"
            else
                echo -e "${YELLOW}⚠ adpm file not found, skipping${NC}"
            fi
        else
            echo -e "${YELLOW}(dry-run)${NC}"
        fi
    done

fi  # SKIP_PACKAGES

# ── Step 6: GitHub Release ────────────────────────────────────
step "[6/7] GitHub Release"

if [[ "$SKIP_GITHUB" == "1" ]] || [[ "$DRY_RUN" == "1" ]]; then
    warn "Skipping GitHub release (--no-github or --dry-run)"
else
    # Tag the source repo
    if [[ -n "$SOURCE_DIR" ]]; then
        echo -ne "  Creating tag ${BOLD}$TAG${NC}... "
        git -C "$SOURCE_DIR" tag -a "$TAG" -m "Release $TAG"
        git -C "$SOURCE_DIR" push origin "$TAG"
        echo -e "${GREEN}✓${NC}"
    fi

    # Collect all artifacts
    mapfile -t ARTIFACTS < <(find "$DIST" -maxdepth 1 -type f \
        \( -name "*.tar.gz" -o -name "*.zip" -o -name "*.deb" \
           -o -name "*.rpm" -o -name "*.adpm" -o -name "installer-*" \) \
        | sort)

    echo ""
    echo -e "  Creating GitHub release ${YELLOW}$TAG${NC} on ${GH_OWNER}/${GH_REPO}..."
    RELEASE_NOTES="Release ${TAG}

## Install

**macOS (Homebrew):**
\`\`\`bash
brew install ${BREW_TAP_OWNER}/tap/${BREW_FORMULA}
\`\`\`

**Linux (curl):**
\`\`\`bash
curl -sSL ${PKG_HOMEPAGE}/install | sh
\`\`\`

**Debian/Ubuntu:**
\`\`\`bash
sudo dpkg -i ${PKG_NAME}_${VERSION}_amd64.deb
\`\`\`

**RHEL/Fedora:**
\`\`\`bash
sudo rpm -i ${PKG_NAME}_${VERSION}_x86_64.rpm
\`\`\`

**ADPM:**
\`\`\`bash
./installer-linux-x86_64
\`\`\`

See [docs](${PKG_HOMEPAGE}/docs/install) for all install options."

    GITHUB_TOKEN="$GITHUB_TOKEN" gh release create "$TAG" \
        --repo "${GH_OWNER}/${GH_REPO}" \
        --title "${PKG_DISPLAY} ${TAG}" \
        --notes "$RELEASE_NOTES" \
        "${ARTIFACTS[@]}"

    ok "GitHub release created: https://github.com/${GH_OWNER}/${GH_REPO}/releases/tag/${TAG}"
    ok "Artifacts uploaded: ${#ARTIFACTS[@]}"
fi

# ── Step 7: Homebrew Tap ──────────────────────────────────────
step "[7/7] Homebrew Tap"

if [[ "$SKIP_BREW" == "1" ]] || [[ -z "$BREW_TAP_OWNER" ]] || [[ "$DRY_RUN" == "1" ]]; then
    warn "Skipping Homebrew tap update"
else
    # Compute SHA256 for macOS tarballs (local files — no download needed)
    echo -e "  Computing SHA256s..."

    SHA_DARWIN_ARM64=""
    SHA_DARWIN_AMD64=""

    for plat in darwin-arm64 darwin-x86_64; do
        ARCHIVE="${TARBALLS[$plat]:-}"
        [[ -z "$ARCHIVE" ]] || [[ ! -f "$ARCHIVE" ]] && continue
        SHA=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')
        if [[ "$plat" == "darwin-arm64" ]]; then
            SHA_DARWIN_ARM64="$SHA"
        else
            SHA_DARWIN_AMD64="$SHA"
        fi
        ok "${plat}: $SHA"
    done

    [[ -z "$SHA_DARWIN_ARM64" ]] && die "No darwin-arm64 tarball found for Homebrew formula"
    [[ -z "$SHA_DARWIN_AMD64" ]] && die "No darwin-x86_64 tarball found for Homebrew formula"

    # Generate formula
    TMP="$(mktemp -d)"
    trap 'rm -rf "$TMP"' EXIT
    FORMULA_FILE="$TMP/${BREW_FORMULA}.rb"

    # Capitalise class name
    CLASS_NAME="$(echo "$BREW_FORMULA" | python3 -c "import sys; n=sys.stdin.read().strip(); print(n[0].upper()+n[1:])")"

    cat > "$FORMULA_FILE" <<FORMULA
class ${CLASS_NAME} < Formula
  desc "${PKG_DESC}"
  homepage "${PKG_HOMEPAGE}"
  version "${VERSION}"
  license "${PKG_LICENSE}"

  on_macos do
    on_arm do
      url "https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_darwin-arm64.tar.gz"
      sha256 "${SHA_DARWIN_ARM64}"

      def install
        bin.install "${BUILD_BINARY}"
      end
    end

    on_intel do
      url "https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_darwin-x86_64.tar.gz"
      sha256 "${SHA_DARWIN_AMD64}"

      def install
        bin.install "${BUILD_BINARY}"
      end
    end
  end

  test do
    system "#{bin}/${BUILD_BINARY}", "--help"
  end
end
FORMULA

    ok "Formula generated: ${BREW_FORMULA}.rb"

    # Clone tap, commit, push
    echo -e "  Pushing to ${BREW_TAP_OWNER}/${BREW_TAP_REPO}..."
    TAP_DIR="$TMP/tap"

    GIT_ASKPASS="" GIT_TERMINAL_PROMPT=0 \
        git clone "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${BREW_TAP_OWNER}/${BREW_TAP_REPO}.git" \
        "$TAP_DIR" --depth=1 -q

    mkdir -p "$TAP_DIR/Formula"
    cp "$FORMULA_FILE" "$TAP_DIR/Formula/${BREW_FORMULA}.rb"

    (
        cd "$TAP_DIR"
        git config user.email "ci@afterdarksys.com"
        git config user.name "After Dark Systems CI"
        git add "Formula/${BREW_FORMULA}.rb"

        if git diff --cached --quiet; then
            warn "Formula unchanged — already up to date"
        else
            git commit -m "chore: Update ${BREW_FORMULA} formula to ${TAG}"
            GIT_ASKPASS="" GIT_TERMINAL_PROMPT=0 \
                git push "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${BREW_TAP_OWNER}/${BREW_TAP_REPO}.git" HEAD:main -q
            ok "Pushed formula to ${BREW_TAP_OWNER}/${BREW_TAP_REPO}"
        fi
    )
fi

# ── Summary ───────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}   ✅  ${PKG_NAME} ${TAG} released!${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  ${BOLD}Artifacts${NC}   $DIST/"
[[ -d "$DIST" ]] && ls -lh "$DIST" | tail -n +2 | awk '{print "    " $NF " (" $5 ")"}' || true
echo ""
echo -e "  ${BOLD}Install (Homebrew):${NC}"
echo -e "    ${YELLOW}brew install ${BREW_TAP_OWNER}/tap/${BREW_FORMULA}${NC}"
echo ""
echo -e "  ${BOLD}Install (curl):${NC}"
echo -e "    ${YELLOW}curl -sSL ${PKG_HOMEPAGE}/install | sh${NC}"
echo ""
echo -e "  ${BOLD}Install (.deb):${NC}"
echo -e "    ${YELLOW}sudo dpkg -i ${PKG_NAME}_${VERSION}_amd64.deb${NC}"
echo ""
echo -e "  ${BOLD}Install (.rpm):${NC}"
echo -e "    ${YELLOW}sudo rpm -i ${PKG_NAME}_${VERSION}_x86_64.rpm${NC}"
echo ""
echo -e "  ${BOLD}Install (ADPM):${NC}"
echo -e "    ${YELLOW}./installer-linux-x86_64${NC}"
echo ""
if [[ "$SKIP_GITHUB" == "0" ]] && [[ "$DRY_RUN" == "0" ]]; then
    echo -e "  ${BOLD}GitHub:${NC} https://github.com/${GH_OWNER}/${GH_REPO}/releases/tag/${TAG}"
    echo ""
fi
