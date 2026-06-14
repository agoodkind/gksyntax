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
# The Swift and Perl grammar submodules commit only their grammar definition
# (and an external scanner), not the generated parser, so each parser is
# produced from the pinned submodule by the tree-sitter CLI. The other grammars
# commit their parser and need no step. The generated files stay inside the
# submodule working tree (gitignored there) and are never committed to this
# repository.
#
# Swift commits its own parser.c in upstream, so after generation the recipe
# restores the tracked tree with `git checkout -- .`. Perl commits no parser.c,
# so its generated parser.c and tree_sitter/ headers must be kept in place and
# the recipe must not reset the Perl submodule tree.
SWIFT_GRAMMAR_DIR := treesitter/grammars/swift/upstream
SWIFT_GRAMMAR_DEF := $(SWIFT_GRAMMAR_DIR)/src/grammar.json
SWIFT_GRAMMAR_PARSER := $(SWIFT_GRAMMAR_DIR)/src/parser.c
PERL_GRAMMAR_DIR := treesitter/grammars/perl/upstream
PERL_GRAMMAR_DEF := $(PERL_GRAMMAR_DIR)/src/grammar.json
PERL_GRAMMAR_PARSER := $(PERL_GRAMMAR_DIR)/src/parser.c
TREE_SITTER_ABI ?= 14
# tree-sitter CLI lands here when the host has none on PATH, so a bare runner
# with only Go can still generate the parsers. Gitignored.
TREE_SITTER_LOCAL_DIR := $(CURDIR)/.bin

.PHONY: grammars

grammars:
	@if [ ! -f "$(SWIFT_GRAMMAR_DEF)" ]; then \
		echo "grammars: $(SWIFT_GRAMMAR_DIR) is empty; run 'git submodule update --init --recursive'"; \
		exit 1; \
	fi
	@if [ ! -f "$(PERL_GRAMMAR_DEF)" ]; then \
		echo "grammars: $(PERL_GRAMMAR_DIR) is empty; run 'git submodule update --init --recursive'"; \
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
	fi; \
	if [ ! -f "$(PERL_GRAMMAR_PARSER)" ] || [ "$(PERL_GRAMMAR_DEF)" -nt "$(PERL_GRAMMAR_PARSER)" ]; then \
		echo "grammars: generating Perl parser (abi $(TREE_SITTER_ABI))"; \
		( cd "$(PERL_GRAMMAR_DIR)" && "$$ts_bin" generate src/grammar.json --abi $(TREE_SITTER_ABI) ); \
	else \
		echo "grammars: Perl parser already generated"; \
	fi

# Compiling, vetting, linting, and govulncheck all build the Swift and Perl
# grammar packages, so they need the generated parsers. The order-only
# prerequisite generates them first on a fresh checkout without forcing
# rebuilds.
build build-check check test lint vet govulncheck: | grammars
