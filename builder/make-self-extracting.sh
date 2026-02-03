#!/bin/bash
# Create self-extracting ADPM installer
# Usage: ./make-self-extracting.sh package.adpm output-installer

set -e

if [ $# -ne 2 ]; then
    echo "Usage: $0 <package.adpm> <output-installer>"
    exit 1
fi

PACKAGE="$1"
OUTPUT="$2"
INSTALLER_SCRIPT="$(dirname $0)/../installer/adpm-install.sh"

if [ ! -f "$PACKAGE" ]; then
    echo "Error: Package not found: $PACKAGE"
    exit 1
fi

if [ ! -f "$INSTALLER_SCRIPT" ]; then
    echo "Error: Installer script not found: $INSTALLER_SCRIPT"
    exit 1
fi

echo "Creating self-extracting installer..."
echo "  Package: $PACKAGE"
echo "  Output: $OUTPUT"

# Combine installer script and package archive
cat "$INSTALLER_SCRIPT" "$PACKAGE" > "$OUTPUT"
chmod +x "$OUTPUT"

SIZE=$(wc -c < "$OUTPUT" | tr -d ' ')
SIZE_MB=$(echo "scale=2; $SIZE / 1024 / 1024" | bc)

echo "✓ Self-extracting installer created: $OUTPUT"
echo "  Size: ${SIZE_MB} MB"
echo
echo "Users can install with:"
echo "  ./$OUTPUT"
