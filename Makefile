# =============================================================================
#  dbsync — cross-platform build + npm packaging
# =============================================================================
#
#  Targets
#    make build          Build static binaries into bin/
#    make pack           Create an npm tarball (npm runs `prepack` -> make build)
#    make publish        Publish to the npm registry (same prepack build)
#    make bump           Bump the version:  make bump BUMP=patch|minor|major
#    make commit         Commit pending changes (npm version needs a clean tree)
#    make push           Push the release commit and tag to origin
#    make gh-release     Create the GitHub release and attach the binaries
#    make release        Commit + bump + publish + push + GitHub release
#    make release-patch  Shorthand for `make release BUMP=patch` (also -minor/-major)
#    make login          Log in to npm (npm >= 9 prints a browser login link)
#    make version        Print the current version
#    make clean          Remove build artifacts
#    make help           Show this help
#
#  Packaging
#    The published package IS this repository root. package.json declares
#    "bin": { "dbsync": "bin/dbsync" } and "files": ["bin/"], so the
#    tarball carries only the launcher plus the prebuilt platform binaries;
#    its `prepack` script runs `make build` before npm packs or publishes.
#    `make publish` first checks npm auth and, when it has a terminal, starts
#    an interactive login instead of failing (npm >= 9 prints a login link).
#
#  Releasing (git-flow)
#    make release BUMP=patch      # git-flow release: 1.0.1 -> 1.0.2
#    make release-patch           # same thing
#    `make release` drives git-flow, which owns the version tag:
#      1. ensure-auth / ensure-gh / ensure-gitflow   log in, check `git flow`
#      2. commit           pending work              (COMMIT=0 to skip)
#      3. release start    git flow release start <next>
#      4. version          npm version <next> --no-git-tag-version, committed
#                          on the release branch
#      5. release finish   merges the release into master, tags it with
#                          <prefix><next>, back-merges into develop and
#                          deletes the release branch
#      6. push             git push origin master develop <tag>
#      7. publish          npm publish (prepack rebuilds bin/)
#      8. gh-release       gh release create --verify-tag + all binaries
#    Net effect: master ends up holding everything from develop, and every
#    release is tagged, published to npm and attached to a GitHub release.
#    The tag prefix comes from `git config gitflow.prefix.versiontag` (this repo
#    sets it to `v`), so RELEASE_TAG always matches what git-flow created.
#    NOTES="..." sets the GitHub release body; otherwise gh generates it.
#
#    `make bump` is the low-level alternative (npm version + its own tag) for
#    work outside git-flow. `make publish`, `make push` and `make gh-release`
#    stay individually runnable.
#
#  Notes
#    * CGO is disabled, so every binary is fully static and portable.
#    * SQLite support uses the pure-Go modernc.org/sqlite driver, which is why
#      we don't need a C compiler or cgo cross toolchains.
#    * Run `make deps` once after changing dependencies (e.g. the first build
#      after the SQLite driver switch) if you want a clean go.mod/go.sum.
# =============================================================================

NAME      := dbsync

# Single source of truth for the version is package.json (npm publishes this
# very manifest, so nothing needs to inject a version anywhere).
# NOTE: keep this lazily expanded (`?=` / `=`), never `:=`, so that a `bump`
# performed earlier in the same run is reflected by `make version` / the bump
# echo instead of the pre-bump value frozen at parse time.
VERSION   ?= $(shell node -p "require('./package.json').version" 2>/dev/null || echo 0.0.0)

# Version bump for `bump` / `release`: patch | minor | major
BUMP      ?=
# `bump` only: 1 = `npm version` creates its own commit + tag, 0 = package.json
# only. `make release` ignores this - git-flow owns the tag.
TAG       ?= 1
# `release` commits pending work first: `git flow release start` refuses a dirty
# working tree ("Working tree contains unstaged changes. Aborting."), as does
# `npm version` with TAG=1. COMMIT=0 skips that commit.
COMMIT    ?= 1
# Message for the pre-release commit made by `make commit` / `make release`.
MSG       ?= chore: pre-release work

# Next version for `make release`, computed from package.json + BUMP.
# MUST stay simply expanded (`:=`), so it is computed once, before the release
# branch rewrites package.json - a lazy `=` would be *re-evaluated* by the later
# recipe lines and start returning one version too high.
NEXT_VERSION := $(shell command -v node >/dev/null 2>&1 && node -e "const p=require('./package.json').version.split('.').map(Number);const t='$(BUMP)';console.log(t=='major'?(p[0]+1)+'.0.0':t=='minor'?p[0]+'.'+(p[1]+1)+'.0':t=='patch'?p[0]+'.'+p[1]+'.'+(p[2]+1):'')" 2>/dev/null; true)

# GitHub release (gh CLI). git-flow creates the version tag, so read its
# configured prefix to guarantee RELEASE_TAG matches the tag that
# `git flow release finish` actually produced (this repo sets the prefix to `v`).
GITFLOW_TAG_PREFIX ?= $(shell git config --get gitflow.prefix.versiontag 2>/dev/null)
RELEASE_TAG ?= $(GITFLOW_TAG_PREFIX)$(VERSION)
# 1 = push the release commit + tag before creating the GitHub release.
PUSH      ?= 1
# Extra arguments for `git push` (e.g. PUSH_ARGS='origin develop').
PUSH_ARGS ?=
# Release notes body; empty lets gh generate them from commits and merged PRs.
NOTES     ?=
NOTES_ARGS = $(if $(NOTES),--notes "$(NOTES)",--generate-notes)

# Extra flags for `npm login`. Empty means npm's default, which on npm >= 9 is
# web auth (prints a browser login link). Use LOGIN_ARGS=--auth-type=legacy for
# the classic username/password/OTP prompts, or --registry=<url> for another
# registry.
LOGIN_ARGS ?=

# Directory that gets packed into the npm tarball: the tracked Node launcher
# (bin/dbsync) plus the cross-compiled platform binaries. The path must match
# the "bin" and "files" entries in package.json.
BIN_DIR   := bin

GO        ?= go
GOFLAGS   ?= -trimpath -mod=mod
LDFLAGS   ?= -s -w

# <os>/<arch> pairs to build. Extend freely, then update bin/dbsync.
PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64

.PHONY: help deps version commit bump release login ensure-auth ensure-gh ensure-gitflow push gh-release build pack publish clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

deps: ## Regenerate go.mod/go.sum (run after dependency changes)
	$(GO) mod tidy

version: ## Print the version from package.json
	@echo "$(VERSION)"

# `npm version` refuses to run on a dirty working tree, so `release` calls this
# first. Stages everything and commits, or does nothing when the tree is clean.
commit: ## Commit pending changes: MSG="..." sets the message (no-op if clean)
	@command -v git >/dev/null 2>&1 || { echo "error: git is required to commit" >&2; exit 1; }
	@git rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "error: not a git working tree" >&2; exit 1; }
	@if [ -z "$$(git status --porcelain)" ]; then \
		echo ">> git: working tree already clean"; \
	elif ! git config user.email >/dev/null 2>&1; then \
		echo "error: git user.email is not configured; set it with:" >&2; \
		echo "       git config user.email you@example.com" >&2; \
		exit 1; \
	else \
		echo ">> git: committing pending changes as \"$(MSG)\""; \
		git status --short; \
		git add -A && git commit -m "$(MSG)"; \
	fi

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

# NOTE ON STRUCTURE: a recipe line containing `$(MAKE)` is executed even under
# `make -n` - that is how recursive make works. So the destructive git/npm lines
# below must never share a line with a `$(MAKE)` call, otherwise `make -n release`
# would perform a real release. Only the commit/publish/gh-release lines carry
# `$(MAKE)`, and each does nothing but recurse.
release: ensure-auth ensure-gh ensure-gitflow ## git-flow release: BUMP=patch|minor|major
	@case "$(BUMP)" in patch|minor|major) ;; *) \
		echo "error: BUMP must be patch, minor or major (got '$(BUMP)')" >&2; \
		echo "usage: make release BUMP=patch|minor|major" >&2; \
		exit 2 ;; \
	esac
	@test -n "$(NEXT_VERSION)" || { echo "error: could not compute the next version from package.json" >&2; exit 1; }
	@echo ">> git-flow: release $(NEXT_VERSION), tag $(GITFLOW_TAG_PREFIX)$(NEXT_VERSION)"
	@if [ "$(COMMIT)" = "1" ]; then $(MAKE) commit; fi
	@git flow release start "$(NEXT_VERSION)"
	@npm version "$(NEXT_VERSION)" --no-git-tag-version
	@git add package.json
	@git commit -m "release $(NEXT_VERSION)"
	@GIT_MERGE_AUTOEDIT=no git flow release finish -m "release $(NEXT_VERSION)" "$(NEXT_VERSION)"
	@if [ "$(PUSH)" = "1" ]; then \
		echo ">> git: pushing master, develop and $(GITFLOW_TAG_PREFIX)$(NEXT_VERSION)"; \
		git push origin master develop "$(GITFLOW_TAG_PREFIX)$(NEXT_VERSION)"; \
	fi
	@$(MAKE) publish
	@$(MAKE) gh-release

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

login: ## Log in to the npm registry (npm >= 9 prints a browser login link)
	npm login $(LOGIN_ARGS)

# Gate for publishing: confirm npm auth and, when running on a terminal, start an
# interactive login rather than failing. Without a TTY it never prompts, so CI
# gets a fast, explicit error instead of a hang.
ensure-auth:
	@if who=$$(npm whoami 2>/dev/null); then \
		echo ">> npm: authenticated as $$who"; \
	elif [ -t 0 ] && [ -t 1 ]; then \
		echo ">> npm: not logged in - starting login"; \
		npm login $(LOGIN_ARGS) || exit 1; \
		who=$$(npm whoami 2>/dev/null) || { echo "error: npm login did not complete" >&2; exit 1; }; \
		echo ">> npm: logged in as $$who"; \
	else \
		echo "error: not logged in to npm and stdin/stdout is not a terminal." >&2; \
		echo "       log in on a terminal:  make login" >&2; \
		echo "       or provide a token:    npm config set //registry.npmjs.org/:_authToken=<TOKEN>" >&2; \
		exit 1; \
	fi

# Same idea as ensure-auth, for the gh CLI that the GitHub release step needs.
ensure-gh:
	@command -v gh >/dev/null 2>&1 || { \
		echo "error: the GitHub CLI (gh) is required for \`make gh-release\`." >&2; \
		echo "       install it from https://cli.github.com" >&2; \
		exit 1; \
	}
	@if gh auth status >/dev/null 2>&1; then \
		echo ">> gh: authenticated"; \
	elif [ -t 0 ] && [ -t 1 ]; then \
		echo ">> gh: not authenticated - starting login"; \
		gh auth login || exit 1; \
		gh auth status >/dev/null 2>&1 || { echo "error: gh auth login did not complete" >&2; exit 1; }; \
		echo ">> gh: authenticated"; \
	else \
		echo "error: gh is not authenticated and stdin/stdout is not a terminal." >&2; \
		echo "       log in on a terminal:  gh auth login" >&2; \
		echo "       or export a token:     export GH_TOKEN=<TOKEN>" >&2; \
		exit 1; \
	fi

# git-flow (AVH edition) drives `make release`. Note the VS Code git-flow
# extension is a separate thing and does not provide this CLI.
ensure-gitflow:
	@git flow version >/dev/null 2>&1 || { \
		echo "error: the git-flow CLI is required for \`make release\`." >&2; \
		echo "       Debian/Ubuntu: apt-get install git-flow" >&2; \
		echo "       macOS:         brew install git-flow-avh" >&2; \
		echo "       https://github.com/gitflow-avh/gitflow" >&2; \
		exit 1; \
	}

# `pack` and `publish` run from the repository root: package.json points "bin"
# at bin/dbsync and restricts "files" to bin/, and its `prepack` script runs
# `make build` first, so the shipped binaries are always freshly built.
pack: ## Create an npm tarball (npm runs `prepack` -> make build first)
	npm pack

publish: ensure-auth ## Publish to the npm registry (npm runs `prepack` -> make build first)
	npm publish

push: ## Push the release commit and the version tag to origin
	@git rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "error: not a git working tree" >&2; exit 1; }
	@git rev-parse -q --verify "refs/tags/$(RELEASE_TAG)" >/dev/null || { \
		echo "error: tag $(RELEASE_TAG) does not exist locally; finish a release first" >&2; \
		exit 1; \
	}
	@echo ">> git: pushing current branch and $(RELEASE_TAG)"
	git push $(PUSH_ARGS)
	git push origin "$(RELEASE_TAG)"

# Creates the GitHub release for RELEASE_TAG and attaches every platform binary.
# Re-runnable: if the release already exists its assets are replaced (--clobber).
# --verify-tag requires the tag to exist on the remote, so `make push` (or a
# manual push) must run first; gh will not silently tag the default branch.
gh-release: ensure-gh ## Create the GitHub release and attach all bin/ binaries
	@test -n "$$(ls -1 $(BIN_DIR)/$(NAME)-* 2>/dev/null)" || { \
		echo "error: no platform binaries in $(BIN_DIR)/; run 'make build' first" >&2; \
		exit 1; \
	}
	@if gh release view "$(RELEASE_TAG)" >/dev/null 2>&1; then \
		echo ">> gh: release $(RELEASE_TAG) exists - replacing its binaries"; \
		gh release upload "$(RELEASE_TAG)" $(BIN_DIR)/$(NAME)-* --clobber; \
	else \
		echo ">> gh: creating release $(RELEASE_TAG)"; \
		gh release create "$(RELEASE_TAG)" $(BIN_DIR)/$(NAME)-* \
			--title "$(RELEASE_TAG)" --verify-tag $(NOTES_ARGS); \
	fi

clean: ## Remove build artifacts (keeps the bin/dbsync launcher)
	@rm -f "$(BIN_DIR)"/$(NAME)-*
	@rm -rf dist

# Convenience: `make build-darwin`, `make build-linux`, `make build-windows`.
build-%:
	@$(MAKE) build PLATFORMS="$(filter $*%,$(PLATFORMS))"
