#!/usr/bin/env bash
# Rebuild the vendored tree-sitter grammar sources under
# treesitter/grammars/<name>/src from the pinned upstream source that
# treesitter/grammars/<name>/upstream.conf records.
#
# Usage: scripts/vendor-grammars.sh [--check] [<name>...]
#
# Without names, the script processes every grammar directory that contains an
# upstream.conf. For each grammar it fetches the pinned commit, generates the
# parser with the pinned tree-sitter CLI when the manifest sets
# parser=generate, and stages src/parser.c, src/scanner.c, src/tree_sitter/*.h,
# and the upstream LICENSE as src/LICENSE. The default mode replaces src/ with
# the staged files. --check compares the staged files with the committed src/
# and exits nonzero on any difference without writing to the repository.
#
# The go build cache keys a cgo package on the files in its own directory. Each
# grammar package compiles src/ through the #include lines in its
# grammar_parser.c and grammar_scanner.c shims, and the cache does not record
# those included files. After this script rewrites src/, run `go clean -cache`
# before building or testing, or the build reuses objects compiled from the
# previous sources.
#
# Requirements: git, curl, and gunzip, plus network access to the upstream
# repositories and the tree-sitter release downloads.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
readonly GRAMMARS_DIR="${REPO_ROOT}/treesitter/grammars"
readonly TOOLS_DIR="${REPO_ROOT}/.bin"
readonly MANIFEST_NAME="upstream.conf"
readonly USAGE="usage: scripts/vendor-grammars.sh [--check] [<name>...]"
readonly VENDORED_FILES=(
    src/parser.c
    src/scanner.c
    src/tree_sitter/alloc.h
    src/tree_sitter/array.h
    src/tree_sitter/parser.h
)

# The EXIT trap removes WORK_DIR wherever the run stopped. WORK_DIR is
# script-global because a function-local variable is out of scope in the trap
# under set -u.
WORK_DIR=""
CHANGED_GRAMMARS=()

cleanup() {
    if [[ -n "${WORK_DIR}" ]]; then
        rm -rf "${WORK_DIR}"
    fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# manifest_value prints the value of one key=value line in a manifest. A
# missing key is an error, and the run stops with the manifest path and key.
manifest_value() {
    local manifest="$1"
    local key="$2"
    local line
    while IFS= read -r line || [[ -n "${line}" ]]; do
        if [[ "${line}" == "${key}="* ]]; then
            printf '%s\n' "${line#"${key}="}"
            return 0
        fi
    done <"${manifest}"
    echo "vendor-grammars: ${manifest} has no ${key}= line" >&2
    return 1
}

# upstream_git runs git without the global and system configuration. Hooks,
# line-ending conversion, and filters from a maintainer's configuration then
# cannot alter the checked-out upstream bytes that the script vendors.
upstream_git() {
    GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git "$@"
}

# fetch_upstream checks out one commit of an upstream repository into a new
# directory and verifies that the checked-out commit is the pinned one.
fetch_upstream() {
    local repository="$1"
    local commit="$2"
    local checkout="$3"
    upstream_git -c init.defaultBranch=main init --quiet "${checkout}"
    upstream_git -C "${checkout}" fetch --quiet --depth 1 "${repository}" "${commit}"
    upstream_git -C "${checkout}" -c advice.detachedHead=false checkout --quiet FETCH_HEAD
    local fetched
    fetched="$(upstream_git -C "${checkout}" rev-parse HEAD)"
    if [[ "${fetched}" != "${commit}" ]]; then
        echo "vendor-grammars: ${repository} checked out ${fetched}, expected ${commit}" >&2
        return 1
    fi
}

# generate_parser installs the pinned tree-sitter CLI under .bin and runs
# `tree-sitter generate` from the checkout root with the pinned ABI.
generate_parser() {
    local checkout="$1"
    local version="$2"
    local abi="$3"
    local tool_dir="${TOOLS_DIR}/tree-sitter-v${version}"
    "${REPO_ROOT}/scripts/install-tree-sitter.sh" "${tool_dir}" "${version}"
    (
        cd "${checkout}"
        "${tool_dir}/tree-sitter" generate src/grammar.json --abi "${abi}"
    )
}

# stage_sources copies the vendored file set and the upstream LICENSE from a
# checkout into a staging directory laid out like the grammar directory.
stage_sources() {
    local checkout="$1"
    local stage="$2"
    local relative
    for relative in "${VENDORED_FILES[@]}"; do
        mkdir -p "$(dirname "${stage}/${relative}")"
        cp "${checkout}/${relative}" "${stage}/${relative}"
    done
    cp "${checkout}/LICENSE" "${stage}/src/LICENSE"
}

# vendor_grammar rebuilds or checks the src/ directory of one grammar.
vendor_grammar() {
    local name="$1"
    local mode="$2"
    local grammar_dir="${GRAMMARS_DIR}/${name}"
    local manifest="${grammar_dir}/${MANIFEST_NAME}"
    if [[ ! -f "${manifest}" ]]; then
        echo "vendor-grammars: ${manifest} does not exist" >&2
        return 1
    fi

    local repository commit parser
    repository="$(manifest_value "${manifest}" repository)"
    commit="$(manifest_value "${manifest}" commit)"
    parser="$(manifest_value "${manifest}" parser)"

    local checkout="${WORK_DIR}/${name}/checkout"
    local stage="${WORK_DIR}/${name}/stage"
    echo "vendor-grammars: ${name}: fetching ${repository} at ${commit}"
    fetch_upstream "${repository}" "${commit}" "${checkout}"

    case "${parser}" in
        upstream)
            echo "vendor-grammars: ${name}: using the parser committed upstream"
            ;;
        generate)
            local version abi
            version="$(manifest_value "${manifest}" tree_sitter_version)"
            abi="$(manifest_value "${manifest}" abi)"
            echo "vendor-grammars: ${name}: generating the parser with tree-sitter v${version} at abi ${abi}"
            generate_parser "${checkout}" "${version}" "${abi}"
            ;;
        *)
            echo "vendor-grammars: ${manifest}: parser=${parser} is neither generate nor upstream" >&2
            return 1
            ;;
    esac

    stage_sources "${checkout}" "${stage}"

    if diff -rq "${stage}/src" "${grammar_dir}/src"; then
        echo "vendor-grammars: ${name}: src/ matches ${commit}"
        return 0
    fi
    if [[ "${mode}" == "check" ]]; then
        echo "vendor-grammars: ${name}: src/ differs from ${commit}. Run scripts/vendor-grammars.sh ${name} to rewrite it." >&2
        return 1
    fi
    rm -rf "${grammar_dir}/src"
    mv "${stage}/src" "${grammar_dir}/src"
    CHANGED_GRAMMARS+=("${name}")
    echo "vendor-grammars: ${name}: rewrote src/ from ${commit}"
}

main() {
    local mode="write"
    local names=()
    local argument
    for argument in "$@"; do
        case "${argument}" in
            --check)
                mode="check"
                ;;
            -*)
                echo "${USAGE}" >&2
                return 2
                ;;
            *)
                names+=("${argument}")
                ;;
        esac
    done

    if [[ ${#names[@]} -eq 0 ]]; then
        local manifest
        for manifest in "${GRAMMARS_DIR}"/*/"${MANIFEST_NAME}"; do
            if [[ -f "${manifest}" ]]; then
                names+=("$(basename "$(dirname "${manifest}")")")
            fi
        done
    fi
    if [[ ${#names[@]} -eq 0 ]]; then
        echo "vendor-grammars: no ${MANIFEST_NAME} under ${GRAMMARS_DIR}" >&2
        return 1
    fi

    WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/vendor-grammars.XXXXXX")"
    local name
    for name in "${names[@]}"; do
        vendor_grammar "${name}" "${mode}"
    done

    if [[ ${#CHANGED_GRAMMARS[@]} -gt 0 ]]; then
        echo "vendor-grammars: rewrote src/ for ${CHANGED_GRAMMARS[*]}. Run 'go clean -cache' before building or testing."
    fi
}

main "$@"
