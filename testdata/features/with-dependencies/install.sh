#!/bin/sh
set -e

# Test feature that depends on simple-marker
echo "Installing feature with-dependencies"

# Verify simple-marker was installed first (if it exists)
if [ -f /tmp/simple-marker ]; then
    echo "simple-marker found - dependency satisfied"
fi

echo "MESSAGE=${MESSAGE:-depends}" >> /tmp/feature-deps-marker
echo "Feature with-dependencies installed successfully"
