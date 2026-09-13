# =============================================================================
#  mysqlsync — cross-platform build + npm packaging
# =============================================================================
#
#  Targets
#    make build          Build static binaries into bin/
#    make pack           Create an npm tarball (npm runs `prepack` -> make build)
#    make publish        Publish to the npm registry (same prepack build)
#    make bump           Bump the version:  make bump BUMP=patch|minor|major
#    make release        Bump + publish in one go
#    make release-patch  Shorthand for `make release BUMP=patch` (also -minor/-major)
#    make version        Print the current version
#    make clean          Remove build artifacts
#    make help           Show this help
#
#  Packaging
#    The published package IS this repository root. package.json declares
#    "bin": { "mysqlsync": "bin/mysqlsync" } and "files": ["bin/"], so the
#    tarball carries only the launcher plus the prebuilt platform binaries;
#    its `prepack` script runs `make build` before npm packs or publishes.
#
#  Releasing
#    make release BUMP=patch      # 1.0.1 -> 1.0.2, then publish
#    make release-patch           # same thing
#    npm version <patch|minor|major> rewrites package.json and (unless TAG=0)
#    creates the matching git commit and vX.Y.Z tag.
#
#  Notes
#    * CGO is disabled, so every binary is fully static and portable.
#    * SQLite support uses the pure-Go modernc.org/sqlite driver, which is why
#      we don't need a C compiler or cgo cross toolchains.
#    * Run `make deps` once after changing dependencies (e.g. the first build
#      after the SQLite driver switch) if you want a clean go.mod/go.sum.
# =============================================================================

NAME      := mysqlsync

# Single source of truth for the version is package.json (npm publishes this
# very manifest, so nothing needs to inject a version anywhere).
# NOTE: keep this lazily expanded (`?=` / `=`), never `:=`, so that a `bump`
# performed earlier in the same run is reflected by `make version` / the bump
# echo instead of the pre-bump value frozen at parse time.
VERSION   ?= $(shell node -p "require('./package.json').version" 2>/dev/null || echo 0.0.0)

# Version bump for `bump` / `release`: patch | minor | major
BUMP      ?=
# 1 = `npm version` also creates the git commit + tag, 0 = rewrite package.json only
TAG       ?= 1

# Directory that gets packed into the npm tarball: the tracked Node launcher
# (bin/mysqlsync) plus the cross-compiled platform binaries. The path must match
# the "bin" and "files" entries in package.json.
BIN_DIR   := bin

GO        ?= go
GOFLAGS   ?= -trimpath -mod=mod
LDFLAGS   ?= -s -w

# <os>/<arch> pairs to build. Extend freely, then update bin/mysqlsync.
PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64

.PHONY: help deps version bump release build pack publish clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

deps: ## Regenerate go.mod/go.sum (run after dependency changes)
	$(GO) mod tidy

version: ## Print the version from package.json
	@echo "$(VERSION)"

bump: ## Bump the version: BUMP=patch|minor|major (TAG=0 skips git commit/tag)
	@case "$(BUMP)" in \
		patch|minor|major) ;; \
		*) echo "error: BUMP must be patch, minor or major (got '$(BUMP)')" >&2; \
		   echo "usage: make bump BUMP=patch|minor|major" >&2; \
		   exit 2 ;; \
	esac
	@if [ "$(TAG)" = "1" ]; then \
		npm version "$(BUMP)"; \
	else \
		npm version "$(BUMP)" --no-git-tag-version; \
	fi
	@echo ">> $(NAME) version is now $(VERSION)"

release: bump ## Bump the version then publish: BUMP=patch|minor|major
	@$(MAKE) publish

release-%: ## Shorthand: make release-patch | release-minor | release-major
	@$(MAKE) release BUMP=$*

build: ## Build static binaries for every platform into bin/
	@mkdir -p "$(BIN_DIR)"
	@test -f "$(BIN_DIR)/$(NAME)" || { \
		echo "error: $(BIN_DIR)/$(NAME) launcher is missing; it is tracked in git" >&2; \
		exit 1; \
	}
	@chmod +x "$(BIN_DIR)/$(NAME)"
	@for p in $(PLATFORMS); do \
		os="$${p%/*}"; \
		arch="$${p#*/}"; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out="$(BIN_DIR)/$(NAME)-$$os-$$arch$$ext"; \
		echo ">> building $$os/$$arch -> $$out"; \
		CGO_ENABLED=0 GOOS="$$os" GOARCH="$$arch" \
			$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o "$$out" . || exit 1; \
	done
	@echo ">> binaries written to $(BIN_DIR)"

# `pack` and `publish` run from the repository root: package.json points "bin"
# at bin/mysqlsync and restricts "files" to bin/, and its `prepack` script runs
# `make build` first, so the shipped binaries are always freshly built.
pack: ## Create an npm tarball (npm runs `prepack` -> make build first)
	npm pack

publish: ## Publish to the npm registry (npm runs `prepack` -> make build first)
	npm publish

clean: ## Remove build artifacts (keeps the bin/mysqlsync launcher)
	@rm -f "$(BIN_DIR)"/$(NAME)-*
	@rm -rf dist

# Convenience: `make build-darwin`, `make build-linux`, `make build-windows`.
build-%:
	@$(MAKE) build PLATFORMS="$(filter $*%,$(PLATFORMS))"
