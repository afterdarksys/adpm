#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════
#   ADPM — Homebrew Formula Generator + Tap Publisher
#   Homage to Todd Bennett III, unixeng
#
#   Generates a Homebrew formula from local .tar.gz artifacts
#   and pushes to a GitHub tap repository.
#
#   Usage:
#     ./builder/gen-formula.sh \
#       --name ss \
#       --version 0.3.1 \
#       --binary ss \
#       --artifacts dist/ss-0.3.1/ \
#       --tap-owner afterdark \
#       --tap-repo homebrew-tap \
#       --formula ss \
#       --github-owner afterdarksys \
#       --github-repo secretserver-cli \
#       --description "SecretServer.io CLI" \
#       --homepage https://secretserver.io \
#       --license Proprietary
#
#   Requires:
#     export HOMEBREW_TAP_TOKEN=ghp_...
# ════════════════════════════════════════════════════════════════
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

die()  { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }
ok()   { echo -e "  ${GREEN}✓${NC} $*"; }
warn() { echo -e "  ${YELLOW}⚠  $*${NC}"; }

# ── Args ──────────────────────────────────────────────────────
PKG_NAME=""
VERSION=""
BINARY_NAME=""
ARTIFACTS_DIR=""
TAP_OWNER=""
TAP_REPO=""
FORMULA=""
GH_OWNER=""
GH_REPO=""
DESCRIPTION="Package"
HOMEPAGE="https://afterdarksys.com"
LICENSE="Proprietary"
DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --name)         PKG_NAME="$2";       shift 2 ;;
        --version)      VERSION="$2";        shift 2 ;;
        --binary)       BINARY_NAME="$2";    shift 2 ;;
        --artifacts)    ARTIFACTS_DIR="$2";  shift 2 ;;
        --tap-owner)    TAP_OWNER="$2";      shift 2 ;;
        --tap-repo)     TAP_REPO="$2";       shift 2 ;;
        --formula)      FORMULA="$2";        shift 2 ;;
        --github-owner) GH_OWNER="$2";       shift 2 ;;
        --github-repo)  GH_REPO="$2";        shift 2 ;;
        --description)  DESCRIPTION="$2";    shift 2 ;;
        --homepage)     HOMEPAGE="$2";       shift 2 ;;
        --license)      LICENSE="$2";        shift 2 ;;
        --dry-run)      DRY_RUN=1;           shift ;;
        *) die "Unknown argument: $1" ;;
    esac
done

[[ -z "$PKG_NAME" ]]       && die "--name required"
[[ -z "$VERSION" ]]        && die "--version required"
[[ -z "$BINARY_NAME" ]]    && die "--binary required"
[[ -z "$ARTIFACTS_DIR" ]]  && die "--artifacts required"
[[ -z "$TAP_OWNER" ]]      && die "--tap-owner required"
[[ -z "$TAP_REPO" ]]       && die "--tap-repo required"
[[ -z "$FORMULA" ]]        && die "--formula required"
[[ -z "$GH_OWNER" ]]       && die "--github-owner required"
[[ -z "$GH_REPO" ]]        && die "--github-repo required"

[[ ! -d "$ARTIFACTS_DIR" ]] && die "Artifacts dir not found: $ARTIFACTS_DIR"
[[ -z "${HOMEBREW_TAP_TOKEN:-}" ]] && die "HOMEBREW_TAP_TOKEN not set"

VERSION="${VERSION#v}"
TAG="v$VERSION"

# Capitalise formula class name
CLASS_NAME="$(python3 -c "n='$FORMULA'; print(n[0].upper()+n[1:])")"

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}   🍺 ADPM — Homebrew Formula Publisher${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  Formula: ${YELLOW}${FORMULA}${NC} (class ${CLASS_NAME})"
echo -e "  Tap:     ${YELLOW}${TAP_OWNER}/${TAP_REPO}${NC}"
echo -e "  Release: ${YELLOW}${TAG}${NC} → ${GH_OWNER}/${GH_REPO}"
echo ""

# ── SHA256 ────────────────────────────────────────────────────
echo -e "${CYAN}${BOLD}[1/3] SHA256 Checksums${NC}"

get_sha() {
    local f="$1"
    if command -v sha256sum &>/dev/null; then
        sha256sum "$f" | awk '{print $1}'
    else
        shasum -a 256 "$f" | awk '{print $1}'
    fi
}

ARM64_TGZ="$ARTIFACTS_DIR/${PKG_NAME}_${VERSION}_darwin-arm64.tar.gz"
AMD64_TGZ="$ARTIFACTS_DIR/${PKG_NAME}_${VERSION}_darwin-x86_64.tar.gz"
LINUX_AMD64_TGZ="$ARTIFACTS_DIR/${PKG_NAME}_${VERSION}_linux-x86_64.tar.gz"
LINUX_ARM64_TGZ="$ARTIFACTS_DIR/${PKG_NAME}_${VERSION}_linux-aarch64.tar.gz"

[[ ! -f "$ARM64_TGZ" ]] && die "Missing: $ARM64_TGZ"
[[ ! -f "$AMD64_TGZ" ]] && die "Missing: $AMD64_TGZ"

SHA_DARWIN_ARM64=$(get_sha "$ARM64_TGZ")
SHA_DARWIN_AMD64=$(get_sha "$AMD64_TGZ")
ok "darwin-arm64:  $SHA_DARWIN_ARM64"
ok "darwin-x86_64: $SHA_DARWIN_AMD64"

# Linux (optional — Homebrew on Linux via Linuxbrew)
SHA_LINUX_AMD64=""
SHA_LINUX_ARM64=""
if [[ -f "$LINUX_AMD64_TGZ" ]]; then
    SHA_LINUX_AMD64=$(get_sha "$LINUX_AMD64_TGZ")
    ok "linux-x86_64:  $SHA_LINUX_AMD64"
fi
if [[ -f "$LINUX_ARM64_TGZ" ]]; then
    SHA_LINUX_ARM64=$(get_sha "$LINUX_ARM64_TGZ")
    ok "linux-aarch64: $SHA_LINUX_ARM64"
fi

# ── Generate Formula ──────────────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}[2/3] Generate Formula${NC}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
FORMULA_FILE="$TMP/${FORMULA}.rb"

# Build Linux block conditionally
LINUX_BLOCK=""
if [[ -n "$SHA_LINUX_AMD64" ]] || [[ -n "$SHA_LINUX_ARM64" ]]; then
    LINUX_BLOCK="
  on_linux do"
    if [[ -n "$SHA_LINUX_ARM64" ]]; then
        LINUX_BLOCK+="
    on_arm do
      if Hardware::CPU.is_64_bit?
        url \"https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_linux-aarch64.tar.gz\"
        sha256 \"${SHA_LINUX_ARM64}\"

        def install
          bin.install \"${BINARY_NAME}\"
        end
      end
    end"
    fi
    if [[ -n "$SHA_LINUX_AMD64" ]]; then
        LINUX_BLOCK+="
    on_intel do
      url \"https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_linux-x86_64.tar.gz\"
      sha256 \"${SHA_LINUX_AMD64}\"

      def install
        bin.install \"${BINARY_NAME}\"
      end
    end"
    fi
    LINUX_BLOCK+="
  end"
fi

cat > "$FORMULA_FILE" <<FORMULA
class ${CLASS_NAME} < Formula
  desc "${DESCRIPTION}"
  homepage "${HOMEPAGE}"
  version "${VERSION}"
  license "${LICENSE}"

  on_macos do
    on_arm do
      url "https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_darwin-arm64.tar.gz"
      sha256 "${SHA_DARWIN_ARM64}"

      def install
        bin.install "${BINARY_NAME}"
      end
    end

    on_intel do
      url "https://github.com/${GH_OWNER}/${GH_REPO}/releases/download/${TAG}/${PKG_NAME}_${VERSION}_darwin-x86_64.tar.gz"
      sha256 "${SHA_DARWIN_AMD64}"

      def install
        bin.install "${BINARY_NAME}"
      end
    end
  end
${LINUX_BLOCK}
  test do
    system "#{bin}/${BINARY_NAME}", "--help"
  end
end
FORMULA

ok "Formula: ${FORMULA}.rb"
echo ""
cat "$FORMULA_FILE" | sed 's/^/    /'
echo ""

if [[ "$DRY_RUN" == "1" ]]; then
    warn "Dry run — not pushing to tap"
    exit 0
fi

# ── Push to Tap ───────────────────────────────────────────────
echo -e "${CYAN}${BOLD}[3/3] Push to ${TAP_OWNER}/${TAP_REPO}${NC}"

TAP_DIR="$TMP/tap"
GIT_ASKPASS="" GIT_TERMINAL_PROMPT=0 \
    git clone "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_OWNER}/${TAP_REPO}.git" \
    "$TAP_DIR" --depth=1 -q

mkdir -p "$TAP_DIR/Formula"
cp "$FORMULA_FILE" "$TAP_DIR/Formula/${FORMULA}.rb"

(
    cd "$TAP_DIR"
    git config user.email "ci@afterdarksys.com"
    git config user.name "After Dark Systems CI"
    git add "Formula/${FORMULA}.rb"

    if git diff --cached --quiet; then
        warn "Formula unchanged — already up to date"
    else
        git commit -m "chore: Update ${FORMULA} formula to ${TAG}"
        GIT_ASKPASS="" GIT_TERMINAL_PROMPT=0 \
            git push "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_OWNER}/${TAP_REPO}.git" HEAD:main -q
        ok "Pushed to ${TAP_OWNER}/${TAP_REPO}"
    fi
)

echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}   ✅  Homebrew formula published: ${TAG}${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  ${BOLD}Install:${NC}  brew install ${TAP_OWNER}/tap/${FORMULA}"
echo -e "  ${BOLD}Upgrade:${NC}  brew upgrade ${FORMULA}"
echo -e "  ${BOLD}Tap:${NC}      https://github.com/${TAP_OWNER}/${TAP_REPO}"
echo ""
