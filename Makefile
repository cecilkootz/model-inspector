# Directory that contains this Makefile, captured while the file is parsed
# and before any include. Recipes cd here and refuse any other pwd -P, so
# `make -f /path/to/Makefile` does not use the caller's working directory.
# override blocks a command-line or environment MI_ROOT from retargeting that.
override MI_ROOT := $(patsubst %/,%,$(dir $(realpath $(lastword $(MAKEFILE_LIST)))))
ifeq ($(MI_ROOT),)
$(error cannot locate the directory containing this Makefile)
endif
export MI_ROOT

# GNU make is required. On macOS, use gmake when /usr/bin/make is BSD make.
#
# Bare `make` runs `test` (override check, vet, and race tests). That is the
# historical default, plus the override check below.
#
# VERSION, COMMIT, FUZZTIME, and BIN may be set in the environment or on the
# make command line. A command-line assignment wins over the environment.
# VERSION and COMMIT are optional -ldflags -X overrides. An empty value, or
# one that is only spaces and tabs, is treated as unset: the binary keeps
# internal/version.Version and the VCS stamp from the Go toolchain
# (runtime/debug.ReadBuildInfo). A set value must match
# ^[0-9A-Za-z][0-9A-Za-z.+_-]*$ (first character is a letter or digit) so it
# cannot change the shell command or the linker command line. FUZZTIME uses
# ^[0-9A-Za-z.+_-]+$. The link line is MI_LDFLAGS, not the C LDFLAGS variable.

override MODULE := origin.cursor.com/cecilkootz/model-inspector

# Command-line variables are exported into recipe environments, and that
# export expands $(shell) and other functions. Drop them so a hostile value
# cannot run during a later recipe. Internal copies are `override` so a
# command-line VERSION_RAW= or MI_LDFLAGS= cannot replace them.
unexport VERSION
unexport COMMIT
unexport FUZZTIME
unexport BIN
unexport VERSION_RAW
unexport COMMIT_RAW
unexport FUZZTIME_RAW
unexport BIN_RAW
unexport MI_LDFLAGS

override VERSION_RAW := $(value VERSION)
override COMMIT_RAW := $(value COMMIT)
override FUZZTIME_RAW := $(value FUZZTIME)
override BIN_RAW := $(value BIN)

override space := $(subst ,, )
override alnum := 0 1 2 3 4 5 6 7 8 9 A B C D E F G H I J K L M N O P Q R S T U V W X Y Z a b c d e f g h i j k l m n o p q r s t u v w x y z
override ok_chars := $(alnum) . + _ -
override remain = $(if $(2),$(call remain,$(subst $(firstword $(2)),,$(1)),$(wordlist 2,200,$(2))),$(1))
# Non-empty when $(1) contains a character outside the allowed set.
# Spaces count; $(if) alone treats a whitespace-only string as empty.
override bad_token = $(subst $(space),x,$(call remain,$(1),$(ok_chars)))
override starts_alnum = $(strip $(foreach c,$(alnum),$(filter $(c)%,$(1))))
override assert_token = $(if $(call bad_token,$(2)),$(error $(1) must match ^[0-9A-Za-z.+_-]+$$))
override assert_ident = $(if $(call bad_token,$(2)),$(error $(1) must match ^[0-9A-Za-z][0-9A-Za-z.+_-]*$$),$(if $(call starts_alnum,$(2)),,$(error $(1) must match ^[0-9A-Za-z][0-9A-Za-z.+_-]*$$)))

ifeq ($(strip $(FUZZTIME_RAW)),)
override FUZZTIME_RAW := 10s
endif
ifeq ($(strip $(BIN_RAW)),)
override BIN_RAW := bin/model-inspector
endif

_ := $(call assert_token,FUZZTIME,$(FUZZTIME_RAW))
override define newline


endef
_ := $(if $(findstring $(newline),$(BIN_RAW)),$(error BIN must be a single line))

# Append -X only after the token check accepts the value. $(error) aborts
# before $(eval), so a rejected value is never written into MI_LDFLAGS.
# Empty and whitespace-only VERSION/COMMIT skip this and stay unset.
override MI_LDFLAGS :=
_ := $(if $(strip $(VERSION_RAW)),$(call assert_ident,VERSION,$(VERSION_RAW))$(eval override MI_LDFLAGS += -X $(MODULE)/internal/version.Version=$(VERSION_RAW)))
_ := $(if $(strip $(COMMIT_RAW)),$(call assert_ident,COMMIT,$(COMMIT_RAW))$(eval override MI_LDFLAGS += -X $(MODULE)/internal/version.Commit=$(COMMIT_RAW)))

export BIN_RAW
export MI_LDFLAGS

# One shell fragment. $$ survives insertion into a recipe.
override define enter_root
cd "$$MI_ROOT" || { echo "make: cannot enter $$MI_ROOT" >&2; exit 1; }; here=$$(pwd -P); if [ "$$here" != "$$MI_ROOT" ]; then echo "make: refusing to run from $$here; makefile directory is $$MI_ROOT" >&2; exit 1; fi
endef

.PHONY: test build clean cover fuzz check-overrides check-env
.DEFAULT_GOAL := test

# Vet and test every package. Synthetic fixtures are generated in-process;
# a clean checkout does not need any external checkpoint. check-overrides
# rejects hostile VERSION/COMMIT values before the linker runs.
test: check-overrides
	@$(enter_root); go vet ./...
	@$(enter_root); go test -race -count=1 ./...

build:
	@set -eu; \
	$(enter_root); \
	mkdir -p -- "$$(dirname -- "$$BIN_RAW")"; \
	if [ -n "$$MI_LDFLAGS" ]; then \
		go build -ldflags "$$MI_LDFLAGS" -o "$$BIN_RAW" ./cmd/model-inspector; \
	else \
		go build -o "$$BIN_RAW" ./cmd/model-inspector; \
	fi

# Delete only this repo's bin/ directory and a regular coverage.out.
# Paths are relative to the Makefile directory, not the caller's cwd.
# BIN is ignored. It is an environment variable as well as a make setting,
# and treating it as an rm path deleted .git, tracked trees, and Makefile.
# A custom binary placed under bin/ is removed with that directory. The only
# rm -rf is bin/ after it resolves (symlinks and "..") to exactly $MI_ROOT/bin.
# A symlinked bin is removed as a symlink and is not followed.
# A directory named coverage.out is left in place; bin/ is still removed.
clean:
	@set -eu; \
	$(enter_root); \
	repo=$$(pwd -P); \
	refused=0; \
	if [ -e coverage.out ] || [ -L coverage.out ]; then \
	  if [ -d coverage.out ] && [ ! -L coverage.out ]; then \
	    echo "make clean: warning: coverage.out is a directory; not removing it" >&2; \
	    refused=1; \
	  else \
	    rm -f -- coverage.out; \
	  fi; \
	fi; \
	if [ -L bin ]; then \
	  rm -f -- bin; \
	elif [ -d bin ]; then \
	  resolved=$$(cd -- bin && pwd -P) || { echo "make clean: cannot resolve bin/" >&2; exit 1; }; \
	  if [ "$$resolved" != "$$repo/bin" ]; then \
	    echo "make clean: refusing to remove bin (resolves to $$resolved)" >&2; \
	    exit 1; \
	  fi; \
	  rm -rf -- bin; \
	fi; \
	if [ "$$refused" -ne 0 ]; then \
	  exit 1; \
	fi

# Expands recipe exports without building or deleting. check-overrides uses it.
check-env:
	@:

cover:
	@$(enter_root); go test -race -count=1 -coverprofile=coverage.out ./...
	@$(enter_root); go tool cover -func=coverage.out

# Go runs one fuzz target per `go test` invocation, so list every Fuzz*
# function and call each package on its own. Regular tests in that package
# are not repeated (-run matches nothing).
fuzz:
	@set -eu; \
	$(enter_root); \
	tmp=$$(mktemp "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	trap 'rm -f "$$tmp" "$$tmp.list"' EXIT; \
	go test ./... -count=1 -list '^Fuzz' > "$$tmp.list"; \
	awk '/^Fuzz/ { names[++n] = $$0; next } /^ok[[:space:]]/ { for (i = 1; i <= n; i++) print $$2, names[i]; n = 0 }' "$$tmp.list" > "$$tmp"; \
	if [ ! -s "$$tmp" ]; then \
		echo "no fuzz targets found" >&2; \
		exit 1; \
	fi; \
	while read -r pkg name; do \
		printf 'fuzz %s %s (%s)\n' "$$pkg" "$$name" "$(FUZZTIME_RAW)"; \
		go test "$$pkg" -count=1 -run '^$$' -fuzz "^$$name$$" -fuzztime "$(FUZZTIME_RAW)" || exit 1; \
	done < "$$tmp"

# Reject VERSION values that would be shell- or linker-evaluated if they
# were interpolated into -ldflags. Runs from `make test`.
check-overrides:
	@set -eu; \
	$(enter_root); \
	tmp=$$(mktemp "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	ran="$$tmp.ran"; \
	spaced=; \
	wtparent=; \
	scratch=; \
	seeded_internal=; \
	restore() { \
	  if [ -n "$$seeded_internal" ]; then \
	    rm -rf "$$MI_ROOT/internal/bin"; \
	    rm -f "$$MI_ROOT/internal/coverage.out"; \
	  fi; \
	  if [ -n "$$spaced" ]; then rm -rf "$$spaced"; fi; \
	  if [ -n "$$scratch" ]; then rm -rf "$$scratch"; fi; \
	  if [ -n "$$wtparent" ]; then \
	    git worktree remove --force "$$wtparent/repo" >/dev/null 2>&1 || true; \
	    git worktree prune >/dev/null 2>&1 || true; \
	    rm -rf "$$wtparent"; \
	    wtparent=; \
	  fi; \
	  rm -f "$$tmp" "$$tmp.out" "$$tmp.script" "$$ran"; \
	}; \
	trap restore EXIT; \
	trap 'restore; exit 130' INT; \
	trap 'restore; exit 143' TERM; \
	clean_make() { \
	  env -u VERSION -u COMMIT -u FUZZTIME -u BIN \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -s --no-print-directory "$$@"; \
	}; \
	reject() { \
	  val=$$1; label=$$2; var=$$3; shift 3; \
	  if clean_make "$$@" "$$var=$$val" >"$$tmp.out" 2>&1; then \
	    echo "$$label was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	  if [ -e "$$ran" ]; then \
	    echo "$$label executed a command" >&2; exit 1; \
	  fi; \
	}; \
	val=$$(printf '%s' '0.1.0$$(shell touch '"$$ran"')'); \
	reject "$$val" shell VERSION build; \
	val=$$(printf '%s' '0.1.0`touch '"$$ran"'`'); \
	reject "$$val" backticks VERSION build; \
	val=$$(printf '%s' '$${HOME}'); \
	reject "$$val" home VERSION build; \
	val=$$(printf '%s' '0.1.0 -X main.injected=yes'); \
	reject "$$val" extra-x VERSION build; \
	printf '#!/bin/sh\ntouch %s\n' "$$ran" > "$$tmp.script"; \
	chmod +x "$$tmp.script"; \
	val=$$(printf '%s' "-linkmode=external -extld=$$tmp.script"); \
	reject "$$val" extld VERSION build; \
	val=$$(printf '%s' 'abc$$(shell touch '"$$ran"')'); \
	reject "$$val" commit-shell COMMIT build; \
	val=$$(printf '%s' -- '-deadbeef'); \
	reject "$$val" leading-dash VERSION build; \
	ldflags_line() { \
	  clean_make -np "$$@" | awk '$$1 == "MI_LDFLAGS" && $$2 == ":=" { print; exit }'; \
	}; \
	out=$$(ldflags_line build); \
	case "$$out" in \
	  *"MI_LDFLAGS :="|*"MI_LDFLAGS := ") ;; \
	  *) echo "default build sets MI_LDFLAGS: [$$out]" >&2; exit 1 ;; \
	esac; \
	out=$$(ldflags_line build VERSION='   '); \
	case "$$out" in \
	  *"MI_LDFLAGS :="|*"MI_LDFLAGS := ") ;; \
	  *) echo "whitespace VERSION was not unset: [$$out]" >&2; exit 1 ;; \
	esac; \
	out=$$(ldflags_line build VERSION=1.2.3); \
	case "$$out" in \
	  *Version=1.2.3*) ;; \
	  *) echo "VERSION override missing from: $$out" >&2; exit 1 ;; \
	esac; \
	case "$$out" in \
	  *Commit=*) echo "COMMIT was injected with VERSION: $$out" >&2; exit 1 ;; \
	esac; \
	out=$$(ldflags_line build COMMIT=abc123); \
	case "$$out" in \
	  *Commit=abc123*) ;; \
	  *) echo "COMMIT override missing from: $$out" >&2; exit 1 ;; \
	esac; \
	out=$$(clean_make -n build); \
	case "$$out" in \
	  *'"$$BIN_RAW"'*) ;; \
	  *) echo "build recipe does not quote BIN_RAW: $$out" >&2; exit 1 ;; \
	esac; \
	spaced=$$(mktemp -d "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	clean_make build BIN="$$spaced/my tool"; \
	test -x "$$spaced/my tool"; \
	if clean_make -n fuzz FUZZTIME='10s;touch' >"$$tmp.out" 2>&1; then \
	  echo "hostile FUZZTIME was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	val=$$(printf '%s' 'bin/$$(shell touch '"$$ran"')'); \
	if ! clean_make check-env BIN="$$val" >"$$tmp.out" 2>&1; then \
	  echo "hostile BIN failed closed: " >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if [ -e "$$ran" ]; then \
	  echo "hostile BIN executed a command" >&2; exit 1; \
	fi; \
	val=$$(printf '%s' '0.1.0$$(shell touch '"$$ran"')'); \
	if ! clean_make check-env VERSION_RAW="$$val" LDFLAGS='-X evil.Injected=yes' MI_LDFLAGS='-X evil.Injected=yes' >"$$tmp.out" 2>&1; then \
	  echo "VERSION_RAW/LDFLAGS override was not ignored: " >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if [ -e "$$ran" ]; then \
	  echo "VERSION_RAW or MI_LDFLAGS executed a command" >&2; exit 1; \
	fi; \
	out=$$(ldflags_line build VERSION_RAW="$$val" LDFLAGS='-X evil.Injected=yes' MI_LDFLAGS='-X evil.Injected=yes'); \
	case "$$out" in \
	  *"MI_LDFLAGS :="|*"MI_LDFLAGS := ") ;; \
	  *) echo "internal ldflags followed a command-line override: [$$out]" >&2; exit 1 ;; \
	esac; \
	scratch=$$(mktemp -d "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	mkdir -p "$$scratch/caller/bin" "$$scratch/caller/sub/bin"; \
	printf keep > "$$scratch/caller/bin/model-inspector"; \
	printf keep > "$$scratch/caller/coverage.out"; \
	printf keep > "$$scratch/caller/sub/bin/model-inspector"; \
	printf keep > "$$scratch/caller/sub/coverage.out"; \
	seed_repo() { \
	  mkdir -p "$$MI_ROOT/bin"; \
	  printf x > "$$MI_ROOT/bin/model-inspector"; \
	  printf x > "$$MI_ROOT/coverage.out"; \
	}; \
	assert_repo_cleaned() { \
	  if [ -e "$$MI_ROOT/bin" ] || [ -L "$$MI_ROOT/bin" ] || [ -e "$$MI_ROOT/coverage.out" ] || [ -L "$$MI_ROOT/coverage.out" ]; then \
	    echo "clean left the makefile tree's bin/ or coverage.out ($$1)" >&2; exit 1; \
	  fi; \
	}; \
	assert_survives() { \
	  label=$$1; dir=$$2; \
	  if [ ! -f "$$dir/bin/model-inspector" ] || [ ! -f "$$dir/coverage.out" ]; then \
	    echo "clean removed $$label bin/ or coverage.out" >&2; exit 1; \
	  fi; \
	  if [ "$$(cat "$$dir/bin/model-inspector")" != keep ] || [ "$$(cat "$$dir/coverage.out")" != keep ]; then \
	    echo "clean modified $$label files" >&2; exit 1; \
	  fi; \
	}; \
	run_clean_from() { \
	  dir=$$1; shift; \
	  env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$dir" -f "$$MI_ROOT/Makefile" -s --no-print-directory clean "$$@" >"$$tmp.out" 2>&1; \
	}; \
	seed_repo; \
	if ! run_clean_from "$$scratch/caller" BIN=.git; then \
	  echo "clean from a scratch directory failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "scratch directory" "$$scratch/caller"; \
	assert_repo_cleaned "scratch directory"; \
	seed_repo; \
	if ! run_clean_from "$$scratch/caller/sub" BIN=bin/.; then \
	  echo "clean from a subdirectory failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "scratch subdirectory" "$$scratch/caller/sub"; \
	assert_survives "scratch directory" "$$scratch/caller"; \
	assert_repo_cleaned "scratch subdirectory"; \
	if [ -e "$$MI_ROOT/internal/bin" ] || [ -e "$$MI_ROOT/internal/coverage.out" ]; then \
	  echo "internal/bin or internal/coverage.out already exists" >&2; exit 1; \
	fi; \
	seeded_internal=1; \
	mkdir -p "$$MI_ROOT/internal/bin"; \
	printf keep > "$$MI_ROOT/internal/bin/model-inspector"; \
	printf keep > "$$MI_ROOT/internal/coverage.out"; \
	seed_repo; \
	if ! ( cd "$$MI_ROOT/internal" && env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -f ../Makefile -s --no-print-directory clean ) >"$$tmp.out" 2>&1; then \
	  echo "clean from internal/ failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "repo subdirectory" "$$MI_ROOT/internal"; \
	assert_repo_cleaned "repo subdirectory"; \
	rm -rf "$$MI_ROOT/internal/bin"; \
	rm -f "$$MI_ROOT/internal/coverage.out"; \
	seeded_internal=; \
	seed_repo; \
	if ! run_clean_from "$$scratch/caller" MI_ROOT="$$scratch/caller"; then \
	  echo "clean with MI_ROOT override failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "MI_ROOT override" "$$scratch/caller"; \
	assert_repo_cleaned "MI_ROOT override"; \
	mkdir -p "$$scratch/tree/bin" "$$scratch/tree/coverage.out"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/tree/Makefile"; \
	printf x > "$$scratch/tree/bin/keep"; \
	if env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$scratch/caller" -f "$$scratch/tree/Makefile" -s --no-print-directory clean >"$$tmp.out" 2>&1; then \
	  echo "directory coverage.out was not refused" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if [ -e "$$scratch/tree/bin" ] || [ -L "$$scratch/tree/bin" ]; then \
	  echo "directory coverage.out stopped bin/ removal" >&2; exit 1; \
	fi; \
	if [ ! -d "$$scratch/tree/coverage.out" ] || [ -L "$$scratch/tree/coverage.out" ]; then \
	  echo "directory coverage.out was removed" >&2; exit 1; \
	fi; \
	if ! grep -q 'coverage.out is a directory' "$$tmp.out"; then \
	  echo "directory coverage.out produced no warning" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "caller during directory coverage.out" "$$scratch/caller"; \
	if ! command -v git >/dev/null 2>&1; then \
	  echo "skipping clean self-test: git is not available" >&2; \
	elif [ ! -e .git ] || ! git rev-parse --verify --quiet HEAD >/dev/null 2>&1; then \
	  echo "skipping clean self-test: HEAD is not available" >&2; \
	else \
	wtparent=$$(mktemp -d "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	if ! git worktree add --detach "$$wtparent/repo" HEAD >"$$tmp.out" 2>&1; then \
	  echo "git worktree add failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	wt=$$wtparent/repo; \
	cp "$$MI_ROOT/Makefile" "$$wt/Makefile"; \
	base_status=$$(git -C "$$wt" status --porcelain); \
	clean_wt() { \
	  env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$wt" -f "$$wt/Makefile" -s --no-print-directory clean "BIN=$$1" >"$$tmp.out" 2>&1; \
	}; \
	printf keep > "$$wtparent/x"; \
	mkdir -p "$$wtparent/outside"; \
	printf sentinel > "$$wtparent/outside/keep"; \
	ln -s "$$wtparent/outside" "$$wt/bin"; \
	clean_wt 'bin/.'; \
	if [ ! -f "$$wtparent/outside/keep" ] || [ -e "$$wt/bin" ] || [ -L "$$wt/bin" ]; then \
	  echo "clean followed a symlinked bin/" >&2; exit 1; \
	fi; \
	seed_clean_targets() { \
	  mkdir -p "$$wt/bin"; \
	  printf x > "$$wt/bin/model-inspector"; \
	  printf x > "$$wt/coverage.out"; \
	}; \
	assert_clean_safe() { \
	  label=$$1; \
	  if grep -q '^rm:' "$$tmp.out"; then \
	    echo "raw rm error for $$label" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	  if [ ! -e "$$wt/.git" ] || [ ! -d "$$wt/internal" ] || [ ! -f "$$wt/Makefile" ]; then \
	    echo "clean removed a protected path for $$label" >&2; exit 1; \
	  fi; \
	  if [ ! -f "$$wtparent/x" ] || [ ! -f "$$wtparent/outside/keep" ]; then \
	    echo "clean removed an outside path for $$label" >&2; exit 1; \
	  fi; \
	  if [ -e "$$wt/bin" ] || [ -L "$$wt/bin" ] || [ -e "$$wt/coverage.out" ] || [ -L "$$wt/coverage.out" ]; then \
	    echo "clean left bin/ or coverage.out for $$label" >&2; exit 1; \
	  fi; \
	  status=$$(git -C "$$wt" status --porcelain); \
	  if [ "$$status" != "$$base_status" ]; then \
	    echo "tracked tree changed for $$label" >&2; printf '%s\n' "$$status" >&2; exit 1; \
	  fi; \
	}; \
	seed_clean_targets; \
	if ! env -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    BIN=.git MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$wt" -f "$$wt/Makefile" -s --no-print-directory clean >"$$tmp.out" 2>&1; then \
	  echo "env BIN=.git clean failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_clean_safe "env BIN=.git"; \
	for b in .git internal Makefile ../x 'bin/../..' "$$wtparent/x" 'bin/.'; do \
	  seed_clean_targets; \
	  if ! clean_wt "$$b"; then \
	    echo "clean failed for BIN=$$b" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	  assert_clean_safe "BIN=$$b"; \
	done; \
	fi
