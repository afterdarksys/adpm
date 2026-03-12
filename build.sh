#!/bin/bash
# Automates the build process for the ADPM Go CLI
set -e

echo "Building ADPM CLI..."

# Ensure dependencies are tidy
go mod tidy

# Build the binary
go build -o adpm cmd/adpm/main.go

# Make sure it's executable
chmod +x adpm

echo "SUCCESS! 'adpm' binary is ready at ./adpm"
echo "You can run it with: ./adpm --help"
