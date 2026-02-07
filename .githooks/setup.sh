#!/bin/bash
#
# Setup git hooks for this repository
#
# Hooks installed:
#   - pre-commit: Auto-updates coverage badge when baseline changes
#   - commit-msg: Validates conventional commit format
#   - pre-push: Runs tests, blocks direct pushes to main
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Setting up git hooks..."

# Configure git to use our hooks directory
git -C "$REPO_ROOT" config core.hooksPath .githooks

# Make hooks executable
chmod +x "$SCRIPT_DIR/pre-commit"
chmod +x "$SCRIPT_DIR/commit-msg"
chmod +x "$SCRIPT_DIR/pre-push"

echo ""
echo "✓ Git hooks installed!"
echo ""
echo "Hooks enabled:"
echo "  pre-commit  - Auto-updates coverage badge"
echo "  commit-msg  - Enforces conventional commit format"
echo "  pre-push    - Runs tests, blocks direct pushes to main"
