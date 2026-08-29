#!/bin/bash
# Automates the build process for the ADPM Go CLI
set -e

echo "Building ADPM CLI..."

# Build the public CLI.
ADPM_BUILD_CACHE="${ADPM_BUILD_CACHE:-${TMPDIR:-/tmp}/adpm-go-build-cache}"
mkdir -p "$ADPM_BUILD_CACHE"
GOCACHE="$ADPM_BUILD_CACHE" go build -o adpm cmd/adpm/main.go

# Make sure it's executable
chmod +x adpm

echo "SUCCESS! 'adpm' is ready at ./adpm"
echo "You can run it with: ./adpm --help"
