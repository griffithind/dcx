#!/bin/bash
set -e

echo "Installing simple-marker feature..."
echo "Message: ${MESSAGE:-Hello World}"

# Create marker file
mkdir -p /tmp/dcx-features
echo "${MESSAGE:-Hello World}" > /tmp/dcx-features/marker.txt

echo "Feature installed successfully"
