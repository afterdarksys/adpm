#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d -t adpm-installer-test.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

export ADPM_PREFIX="$WORK/prefix"
export ADPM_DB="$WORK/db"
BUILDER="$ROOT/builder/adpm-build.py"
INSTALLER="$ROOT/installer/adpm-install.sh"

platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"
    case "$os-$arch" in
        darwin-arm64|darwin-aarch64) echo darwin-arm64 ;;
        darwin-x86_64) echo darwin-x86_64 ;;
        linux-aarch64|linux-arm64) echo linux-aarch64 ;;
        linux-x86_64) echo linux-x86_64 ;;
        *) return 1 ;;
    esac
}

build_pkg() {
    local name="$1" version="$2" binary="$3" output="$4"
    python3 "$BUILDER" --name "$name" --version "$version" \
        --platform "$(platform)" --binaries "$binary" --output "$output" >/dev/null
}

build_pkg lifecycle 1.0.0 /bin/echo "$WORK/v1"
bash "$INSTALLER" "$WORK/v1/lifecycle-1.0.0.adpm" >/dev/null
test -x "$ADPM_PREFIX/bin/echo"

build_pkg lifecycle 2.0.0 /bin/cat "$WORK/v2"
bash "$INSTALLER" --upgrade "$WORK/v2/lifecycle-2.0.0.adpm" >/dev/null
test -x "$ADPM_PREFIX/bin/cat"
test ! -e "$ADPM_PREFIX/bin/echo"

bash "$INSTALLER" --rollback lifecycle >/dev/null
test -x "$ADPM_PREFIX/bin/echo"
test ! -e "$ADPM_PREFIX/bin/cat"

python3 "$BUILDER" --name dependency-failure --version 1.0.0 \
    --platform "$(platform)" --dependency 'missing-package@>=1.0' --output "$WORK/dependency" >/dev/null
if bash "$INSTALLER" "$WORK/dependency/dependency-failure-1.0.0.adpm" >/dev/null 2>&1; then
    echo "dependency rejection test failed" >&2
    exit 1
fi

build_pkg verified 1.0.0 /bin/echo "$WORK/verified"
ARCHIVE="$WORK/verified/verified-1.0.0.adpm"
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$ARCHIVE" > "$ARCHIVE.sha256"
else
    shasum -a 256 "$ARCHIVE" > "$ARCHIVE.sha256"
fi
bash "$INSTALLER" --verify "$ARCHIVE" >/dev/null
bash "$INSTALLER" --uninstall verified >/dev/null
cp "$ARCHIVE" "$WORK/tampered.adpm"
cp "$ARCHIVE.sha256" "$WORK/tampered.adpm.sha256"
printf 'tampered' >> "$WORK/tampered.adpm"
if bash "$INSTALLER" --verify "$WORK/tampered.adpm" >/dev/null 2>&1; then
    echo "tampered checksum verification test failed" >&2
    exit 1
fi
if bash "$INSTALLER" --verify-required "$ARCHIVE" >/dev/null 2>&1; then
    echo "strict signature verification test failed" >&2
    exit 1
fi

case "$(platform)" in
    darwin-*) incompatible="linux-x86_64" ;;
    *) incompatible="darwin-x86_64" ;;
esac
python3 "$BUILDER" --name incompatible --version 1.0.0 \
    --platform "$incompatible" --binaries /bin/echo --output "$WORK/incompatible" >/dev/null
if bash "$INSTALLER" "$WORK/incompatible/incompatible-1.0.0.adpm" >/dev/null 2>&1; then
    echo "incompatible platform test failed" >&2
    exit 1
fi

bash "$INSTALLER" --uninstall lifecycle >/dev/null
echo "installer integration tests passed"
