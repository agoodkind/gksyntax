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
# Vendored grammars
# ---------------------------------------------------------------------------
# The Swift and Dart grammar C sources are committed under
# treesitter/grammars/<name>/src. Build, lint, and test targets read them from
# the checkout and need neither git submodules nor the tree-sitter CLI. A
# consumer compiles the same sources from the Go module zip. grammars is a
# manual target that runs scripts/vendor-grammars.sh to rebuild those sources
# from the upstream commit and generator settings in each grammar's
# upstream.conf. No build, lint, or test target depends on it. Commit the
# rewritten src/ files after running it.
.PHONY: grammars

grammars:
	./scripts/vendor-grammars.sh
