#!/bin/bash
set -e

# Test feature that writes options to a marker file
echo "Installing feature with-options"
echo "GREETING=${GREETING:-Hello}" >> /tmp/feature-options-marker
echo "ENABLED=${ENABLED:-true}" >> /tmp/feature-options-marker
echo "COUNT=${COUNT:-1}" >> /tmp/feature-options-marker
echo "Feature with-options installed successfully"
