#!/usr/bin/env bash
# Usage: curl -fsSL https://raw.githubusercontent.com/Nova-Stark/mittodrop-cli/master/install.sh | bash
set -e

# Repository configuration
REPO="Nova-Stark/mittodrop-cli"
PROJECT="mittodrop"
BINARY="mitto"
ALIAS="mittodrop"

# Color helpers
BOLD="$(tput bold 2>/dev/null || echo '')"
GREEN="$(tput setaf 2 2>/dev/null || echo '')"
YELLOW="$(tput setaf 3 2>/dev/null || echo '')"
RED="$(tput setaf 1 2>/dev/null || echo '')"
RESET="$(tput sgr0 2>/dev/null || echo '')"

log_info() { echo "${GREEN}==>${RESET} ${BOLD}$*${RESET}"; }
log_warn() { echo "${YELLOW}==>${RESET} ${YELLOW}$*${RESET}"; }
log_err()  { echo "${RED}==>${RESET} ${RED}$*${RESET}" >&2; }

# 1. Detect operating system
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        log_err "Unsupported operating system: $OS. Please install manually."
        exit 1
        ;;
esac

# 2. Detect CPU architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        log_err "Unsupported architecture: $ARCH. Please build from source."
        exit 1
        ;;
esac

log_info "Detected platform: ${OS}/${ARCH}"

# 3. Query latest release version
log_info "Fetching latest release information..."
LATEST_TAG="$(curl -fsSL -H "Accept: application/vnd.github.v3+json" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -n 1 | cut -d '"' -f 4)"

if [ -z "$LATEST_TAG" ]; then
    # Fallback to redirect resolution if GitHub API rate-limited
    LATEST_TAG="$(curl -fsSI "https://github.com/${REPO}/releases/latest" 2>/dev/null | grep -i '^location:' | awk -F'/' '{print $NF}' | tr -d '\r\n')"
fi

if [ -z "$LATEST_TAG" ]; then
    log_err "Failed to find latest release tag. Check internet connection or repository status."
    exit 1
fi

VERSION="${LATEST_TAG#v}"
log_info "Target release: ${LATEST_TAG} (v${VERSION})"

# 4. Construct asset filename and download URL
ARCHIVE_NAME="${PROJECT}_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${ARCHIVE_NAME}"

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

log_info "Downloading ${ARCHIVE_NAME}..."
if ! curl -fsSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${ARCHIVE_NAME}"; then
    log_err "Download failed from: $DOWNLOAD_URL"
    exit 1
fi

# 5. Extract archive
log_info "Extracting binary..."
tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "$TMP_DIR"

if [ ! -f "${TMP_DIR}/${BINARY}" ]; then
    log_err "Binary '${BINARY}' not found in downloaded archive."
    exit 1
fi

# 6. Determine target install directory
INSTALL_DIR="/usr/local/bin"
USE_SUDO=""

if [ -w "$INSTALL_DIR" ]; then
    USE_SUDO=""
elif command -v sudo >/dev/null 2>&1; then
    USE_SUDO="sudo"
else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

log_info "Installing ${BINARY} into ${INSTALL_DIR}..."
$USE_SUDO install -m 755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

# Create convenience alias symlink mittodrop -> mitto
if [ "$BINARY" != "$ALIAS" ]; then
    $USE_SUDO ln -sf "${INSTALL_DIR}/${BINARY}" "${INSTALL_DIR}/${ALIAS}" 2>/dev/null || true
fi

# 7. Check PATH and confirm installation
echo ""
log_info "${GREEN}Installation successful!${RESET}"
echo ""

case ":$PATH:" in
    *:"$INSTALL_DIR":*) ;;
    *)
        log_warn "Notice: ${INSTALL_DIR} is not currently in your PATH."
        log_warn "Add it to your shell profile (~/.bashrc or ~/.zshrc):"
        echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
        echo ""
        ;;
esac

echo "You can now run:"
echo "  ${BOLD}mitto --help${RESET}"
echo "  ${BOLD}mitto -v${RESET}"
