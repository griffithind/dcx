#!/bin/sh
set -e

# dcx installer
# Usage: curl -fsSL https://raw.githubusercontent.com/griffithind/dcx/main/install.sh | sh

REPO="griffithind/dcx"
INSTALL_DIR="${HOME}/.local/bin"

# Colors (only if terminal supports it)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

info() {
    printf "${GREEN}==>${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}Warning:${NC} %s\n" "$1"
}

error() {
    printf "${RED}Error:${NC} %s\n" "$1" >&2
    exit 1
}

# Detect OS
detect_os() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        darwin) OS="darwin" ;;
        linux) OS="linux" ;;
        *) error "Unsupported operating system: $OS" ;;
    esac
}

# Detect architecture
detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac
}

# Check for required commands
check_deps() {
    for cmd in curl grep awk mktemp; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            error "Required command not found: $cmd"
        fi
    done
}

# sha256 prints the SHA-256 of a file (sha256sum on Linux, shasum on macOS)
sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        error "Neither sha256sum nor shasum found; cannot verify the download"
    fi
}

# Get latest release version
get_latest_version() {
    LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | head -1 | cut -d'"' -f4)
    if [ -z "$LATEST" ]; then
        error "Failed to get latest release version"
    fi
}

# Main installation
main() {
    info "Installing dcx..."

    check_deps
    detect_os
    detect_arch
    get_latest_version

    BINARY="dcx-${OS}-${ARCH}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST}/${BINARY}"

    info "Detected: ${OS}/${ARCH}"
    info "Latest version: ${LATEST}"
    info "Downloading ${BINARY}..."

    # Download into a temporary directory and verify against the release's
    # checksums.txt before installing anything.
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR}"' EXIT
    if ! curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${BINARY}"; then
        error "Failed to download ${BINARY}"
    fi
    if ! curl -fsSL "https://github.com/${REPO}/releases/download/${LATEST}/checksums.txt" -o "${TMP_DIR}/checksums.txt"; then
        error "Failed to download checksums.txt"
    fi
    EXPECTED=$(awk -v f="${BINARY}" '$2 == f || $2 == "*" f { print $1 }' "${TMP_DIR}/checksums.txt")
    if [ -z "${EXPECTED}" ]; then
        error "checksums.txt has no entry for ${BINARY}"
    fi
    ACTUAL=$(sha256 "${TMP_DIR}/${BINARY}")
    if [ "${EXPECTED}" != "${ACTUAL}" ]; then
        error "Checksum mismatch for ${BINARY} (expected ${EXPECTED}, got ${ACTUAL})"
    fi
    info "Checksum verified"

    mkdir -p "${INSTALL_DIR}"
    chmod +x "${TMP_DIR}/${BINARY}"
    mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/dcx"

    info "Installed dcx to ${INSTALL_DIR}/dcx"

    # Verify installation
    if "${INSTALL_DIR}/dcx" --version >/dev/null 2>&1; then
        VERSION=$("${INSTALL_DIR}/dcx" --version 2>&1 | head -1)
        info "Verified: ${VERSION}"
    fi

    # Check if install dir is in PATH
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            echo ""
            warn "${INSTALL_DIR} is not in your PATH"
            echo ""
            echo "Add it to your shell configuration:"
            echo ""
            echo "  # For bash (~/.bashrc):"
            echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
            echo ""
            echo "  # For zsh (~/.zshrc):"
            echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\""
            echo ""
            echo "Then restart your shell or run: source ~/.bashrc (or ~/.zshrc)"
            ;;
    esac

    echo ""
    info "Installation complete!"
    echo ""
    echo "Get started:"
    echo "  dcx --help"
    echo ""
    echo "To upgrade later:"
    echo "  dcx upgrade"
}

main "$@"
