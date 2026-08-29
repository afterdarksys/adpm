#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TEST_TMP="$(mktemp -d -t adpm-tests.XXXXXX)"
trap 'rm -rf "$TEST_TMP"' EXIT

export PYTHONPYCACHEPREFIX="$TEST_TMP/pycache"
export GOCACHE="$TEST_TMP/go-cache"

echo "==> Python builder unit tests"
python3 -m unittest discover -s tests -p 'test_builder.py' -v

echo "==> Go unit and conversion-matrix tests"
go test ./...

echo "==> Installer integration tests"
tests/installer_integration.sh

echo "All ADPM tests passed"
