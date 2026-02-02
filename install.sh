#!/bin/sh
set -e

# Fiso CLI installer
# Usage: curl -fsSL https://raw.githubusercontent.com/lsm/fiso/main/install.sh | sh

REPO="lsm/fiso"
INSTALL_DIR="${FISO_INSTALL_DIR:-/usr/local/bin}"
VERSION="${FISO_VERSION:-latest}"

main() {
    detect_platform
    resolve_version
    download_and_install
    verify
    print_success
}

detect_platform() {
    OS="$(uname -s)"
    ARCH="$(uname -m)"

    case "$OS" in
        Linux)  OS="linux" ;;
        Darwin) OS="darwin" ;;
        *)      echo "Error: unsupported OS: $OS" >&2; exit 1 ;;
    esac

    case "$ARCH" in
        x86_64)  ARCH="amd64" ;;
        amd64)   ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        arm64)   ARCH="arm64" ;;
        *)       echo "Error: unsupported architecture: $ARCH" >&2; exit 1 ;;
    esac
}

resolve_version() {
    if [ "$VERSION" = "latest" ]; then
        VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' \
            | sed 's/.*"tag_name": *"//;s/".*//')"

        if [ -z "$VERSION" ]; then
            echo "Error: could not determine latest version" >&2
            exit 1
        fi
    fi
}

download_and_install() {
    TARBALL="fiso_${OS}_${ARCH}.tar.gz"
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

    TMPDIR="$(mktemp -d)"
    trap 'rm -rf "$TMPDIR"' EXIT

    echo "Downloading fiso ${VERSION} (${OS}/${ARCH})..."
    if ! curl -fsSL -o "${TMPDIR}/${TARBALL}" "$URL"; then
        echo "Error: download failed. Check that version ${VERSION} exists at:" >&2
        echo "  https://github.com/${REPO}/releases" >&2
        exit 1
    fi

    tar -xzf "${TMPDIR}/${TARBALL}" -C "$TMPDIR"

    if [ ! -f "${TMPDIR}/fiso" ]; then
        echo "Error: fiso binary not found in archive" >&2
        exit 1
    fi

    mkdir -p "$INSTALL_DIR"

    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMPDIR}/fiso" "${INSTALL_DIR}/fiso"
    else
        echo "Installing to ${INSTALL_DIR} (requires sudo)..."
        sudo mv "${TMPDIR}/fiso" "${INSTALL_DIR}/fiso"
    fi

    chmod +x "${INSTALL_DIR}/fiso"
}

verify() {
    if ! "${INSTALL_DIR}/fiso" --help > /dev/null 2>&1; then
        echo "Warning: installed binary does not appear to work" >&2
    fi
}

print_success() {
    echo ""
    echo "fiso ${VERSION} installed to ${INSTALL_DIR}/fiso"
    echo ""
    echo "Get started:"
    echo "  mkdir my-project && cd my-project"
    echo "  fiso init"
    echo "  fiso dev"
}

main
