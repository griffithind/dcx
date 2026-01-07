#!/bin/sh
set -e

echo "Installing with-mounts feature..."

# Create marker file to verify feature was installed
mkdir -p /tmp/dcx-features
echo "with-mounts installed" > /tmp/dcx-features/with-mounts-marker.txt

echo "Feature installed successfully"
