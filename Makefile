# `make help` is the canonical source of truth for every target this repo
# supports. Run it before adding anything new. Lint, build, test, deadcode,
# release, baseline, and service-install all live in the central go-makefile
# pipeline fetched at parse time. Do NOT add project-local lint, deadcode,
# audit, fmt, vet, or staticcheck targets here. They duplicate the central
# pipeline and let agents bypass strict rules.

# Library mode: build/install no-op; lint/vet/test from go.mk still apply.
LIBRARY := 1

# Pipeline modules.
GO_MK_MODULES := go-build.mk

# bootstrap.mk fetches go.mk + golangci.yml + every module in GO_MK_MODULES
# at parse time and -includes them. Update path: edit go-makefile/bootstrap.mk,
# then refresh consumer copies (one-off cp; not enshrined as infrastructure).
include bootstrap.mk

.DEFAULT_GOAL := check

# ---------------------------------------------------------------------------
# Grammar generation
# ---------------------------------------------------------------------------
# The Swift grammar submodule commits only its grammar definition, not the
# generated parser, so the parser is produced from the pinned submodule by the
# tree-sitter CLI. The other grammars commit their parser and need no step. The
# generated files stay inside the submodule working tree (gitignored there) and
# are never committed to this repository.
SWIFT_GRAMMAR_DIR := treesitter/grammars/swift/upstream
SWIFT_GRAMMAR_DEF := $(SWIFT_GRAMMAR_DIR)/src/grammar.json
SWIFT_GRAMMAR_PARSER := $(SWIFT_GRAMMAR_DIR)/src/parser.c
TREE_SITTER_ABI ?= 14
# tree-sitter CLI lands here when the host has none on PATH, so a bare runner
# with only Go can still generate the Swift parser. Gitignored.
TREE_SITTER_LOCAL_DIR := $(CURDIR)/.bin

.PHONY: grammars

grammars:
	@if [ ! -f "$(SWIFT_GRAMMAR_DEF)" ]; then \
		echo "grammars: $(SWIFT_GRAMMAR_DIR) is empty; run 'git submodule update --init --recursive'"; \
		exit 1; \
	fi
	@ts_bin="$$(command -v tree-sitter 2>/dev/null || true)"; \
	if [ -z "$$ts_bin" ]; then \
		./scripts/install-tree-sitter.sh "$(TREE_SITTER_LOCAL_DIR)"; \
		ts_bin="$(TREE_SITTER_LOCAL_DIR)/tree-sitter"; \
	fi; \
	if [ ! -f "$(SWIFT_GRAMMAR_PARSER)" ] || [ "$(SWIFT_GRAMMAR_DEF)" -nt "$(SWIFT_GRAMMAR_PARSER)" ]; then \
		echo "grammars: generating Swift parser (abi $(TREE_SITTER_ABI))"; \
		( cd "$(SWIFT_GRAMMAR_DIR)" && "$$ts_bin" generate src/grammar.json --abi $(TREE_SITTER_ABI) ); \
		git -C "$(SWIFT_GRAMMAR_DIR)" checkout -- . >/dev/null 2>&1 || true; \
	else \
		echo "grammars: Swift parser already generated"; \
	fi

# Compiling, vetting, linting, and govulncheck all build the Swift grammar
# package, so they need the generated parser. The order-only prerequisite
# generates it first on a fresh checkout without forcing rebuilds.
build build-check check test lint vet govulncheck: | grammars
