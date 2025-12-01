#!/bin/bash

# Script to run unit tests for the lib package
# This script is called by the Git pre-push hook

set -e

echo "Running unit tests..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    echo "Visit: https://go.dev/dl/"
    exit 1
fi

# Run all tests
go test -v ./...

echo "Tests completed!"
