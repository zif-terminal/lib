#!/bin/bash

# Setup script to configure Git to use hooks from the tracked hooks/ directory
# This makes hooks fully automatic - no manual installation needed!

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Configuring Git to use hooks from hooks/ directory..."

# Set Git to use hooks from the tracked hooks/ directory
cd "$REPO_ROOT"
git config core.hooksPath hooks

if [ $? -eq 0 ]; then
    echo "✅ Git hooks configured successfully!"
    echo ""
    echo "Hooks are now fully automatic:"
    echo "  - Pre-push hook will run unit tests automatically before push"
    echo "  - No manual installation needed"
    echo "  - Hooks are tracked in git and stay up-to-date automatically"
else
    echo "❌ Failed to configure Git hooks"
    exit 1
fi
