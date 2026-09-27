#!/usr/bin/env bash
# Install one tree-sitter CLI release into a destination directory. The
# scripts/vendor-grammars.sh script passes the CLI version that a grammar's
# upstream.conf records, because `tree-sitter generate` output differs between
# CLI releases and the vendored parser must be reproducible byte for byte.
# Downloads the official prebuilt release binary so a fresh macOS or
# Debian/Ubuntu host needs no npm, cargo, or brew.
set -euo pipefail

readonly DEST_DIR="${1:?usage: install-tree-sitter.sh <dest-dir> <version>}"
readonly TREE_SITTER_VERSION="${2:?usage: install-tree-sitter.sh <dest-dir> <version>}"
readonly DEST_BIN="${DEST_DIR}/tree-sitter"
readonly RELEASE_BASE="https://github.com/tree-sitter/tree-sitter/releases/download"

# Script-global so the EXIT trap can clean it up regardless of where the run
# stopped; a function-local would be out of scope in the trap under set -u.
GZ_TMP=""

cleanup() {
    if [[ -n "${GZ_TMP}" ]]; then
        rm -f "${GZ_TMP}"
    fi
}
trap cleanup EXIT

detect_os() {
    local kernel
    kernel="$(uname -s)"
    case "${kernel}" in
        Linux) echo "linux" ;;
        Darwin) echo "macos" ;;
        *) echo "unsupported:${kernel}" ;;
    esac
}

detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64 | amd64) echo "x64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *) echo "unsupported:${machine}" ;;
    esac
}

# reported_version prints the version a tree-sitter binary reports, which is the
# second field of `tree-sitter --version` ("tree-sitter 0.26.9").
reported_version() {
    local binary="$1"
    local output version
    output="$("${binary}" --version)"
    read -r _ version _ <<<"${output}"
    printf '%s\n' "${version}"
}

main() {
    if [[ -x "${DEST_BIN}" ]]; then
        local present
        present="$(reported_version "${DEST_BIN}")"
        if [[ "${present}" == "${TREE_SITTER_VERSION}" ]]; then
            echo "install-tree-sitter: ${DEST_BIN} v${present} already present"
            return 0
        fi
        echo "install-tree-sitter: replacing ${DEST_BIN} v${present} with v${TREE_SITTER_VERSION}"
    fi

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"
    if [[ "${os}" == unsupported:* || "${arch}" == unsupported:* ]]; then
        echo "install-tree-sitter: no prebuilt tree-sitter release for ${os} ${arch}" >&2
        return 1
    fi

    local asset url
    asset="tree-sitter-${os}-${arch}.gz"
    url="${RELEASE_BASE}/v${TREE_SITTER_VERSION}/${asset}"
    mkdir -p "${DEST_DIR}"
    GZ_TMP="$(mktemp)"

    echo "install-tree-sitter: downloading tree-sitter v${TREE_SITTER_VERSION} (${os}/${arch})"
    curl -fsSL "${url}" -o "${GZ_TMP}" || {
        echo "install-tree-sitter: download failed: ${url}" >&2
        return 1
    }
    gunzip -c "${GZ_TMP}" >"${DEST_BIN}"
    chmod +x "${DEST_BIN}"

    local installed
    installed="$(reported_version "${DEST_BIN}")"
    if [[ "${installed}" != "${TREE_SITTER_VERSION}" ]]; then
        echo "install-tree-sitter: ${DEST_BIN} reports v${installed}, expected v${TREE_SITTER_VERSION}" >&2
        return 1
    fi
    echo "install-tree-sitter: installed ${DEST_BIN} v${installed}"
}

main
