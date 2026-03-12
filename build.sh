#!/bin/bash
# Automates the build process for the ADPM Go CLI
set -e

echo "Building ADPM CLI..."

# Ensure dependencies are tidy
go mod tidy

# Build the binary (avoid conflict with adpm/ directory)
go build -o adpm_cli cmd/adpm/main.go

# Make sure it's executable
chmod +x adpm_cli

echo "SUCCESS! 'adpm_cli' binary is ready at ./adpm_cli"
echo "You can run it with: ./adpm_cli --help"
