# Directory that contains this Makefile, captured while the file is parsed
# and before any include. $(value) keeps '$' in the path from being expanded.
# Whitespace is rejected before dir/patsubst, which split arguments on spaces
# and would otherwise point MI_ROOT at a different directory. The directory
# must match [A-Za-z0-9._/+-] (no quotes, '$', backticks, or glob characters).
# Recipes never interpolate the path: they use the exported "$$MI_ROOT".
override space := $(subst ,, )
ifneq ($(words $(value MAKEFILE_LIST)),1)
$(error refusing Makefile path)
endif
override MI_MAKEFILE := $(realpath $(value MAKEFILE_LIST))
ifneq ($(words $(MI_MAKEFILE)),1)
$(error refusing Makefile path)
endif
override MI_ROOT := $(patsubst %/,%,$(dir $(MI_MAKEFILE)))
override _mi := $(MI_ROOT)
override _mi := $(subst 0,,$(_mi))
override _mi := $(subst 1,,$(_mi))
override _mi := $(subst 2,,$(_mi))
override _mi := $(subst 3,,$(_mi))
override _mi := $(subst 4,,$(_mi))
override _mi := $(subst 5,,$(_mi))
override _mi := $(subst 6,,$(_mi))
override _mi := $(subst 7,,$(_mi))
override _mi := $(subst 8,,$(_mi))
override _mi := $(subst 9,,$(_mi))
override _mi := $(subst A,,$(_mi))
override _mi := $(subst B,,$(_mi))
override _mi := $(subst C,,$(_mi))
override _mi := $(subst D,,$(_mi))
override _mi := $(subst E,,$(_mi))
override _mi := $(subst F,,$(_mi))
override _mi := $(subst G,,$(_mi))
override _mi := $(subst H,,$(_mi))
override _mi := $(subst I,,$(_mi))
override _mi := $(subst J,,$(_mi))
override _mi := $(subst K,,$(_mi))
override _mi := $(subst L,,$(_mi))
override _mi := $(subst M,,$(_mi))
override _mi := $(subst N,,$(_mi))
override _mi := $(subst O,,$(_mi))
override _mi := $(subst P,,$(_mi))
override _mi := $(subst Q,,$(_mi))
override _mi := $(subst R,,$(_mi))
override _mi := $(subst S,,$(_mi))
override _mi := $(subst T,,$(_mi))
override _mi := $(subst U,,$(_mi))
override _mi := $(subst V,,$(_mi))
override _mi := $(subst W,,$(_mi))
override _mi := $(subst X,,$(_mi))
override _mi := $(subst Y,,$(_mi))
override _mi := $(subst Z,,$(_mi))
override _mi := $(subst a,,$(_mi))
override _mi := $(subst b,,$(_mi))
override _mi := $(subst c,,$(_mi))
override _mi := $(subst d,,$(_mi))
override _mi := $(subst e,,$(_mi))
override _mi := $(subst f,,$(_mi))
override _mi := $(subst g,,$(_mi))
override _mi := $(subst h,,$(_mi))
override _mi := $(subst i,,$(_mi))
override _mi := $(subst j,,$(_mi))
override _mi := $(subst k,,$(_mi))
override _mi := $(subst l,,$(_mi))
override _mi := $(subst m,,$(_mi))
override _mi := $(subst n,,$(_mi))
override _mi := $(subst o,,$(_mi))
override _mi := $(subst p,,$(_mi))
override _mi := $(subst q,,$(_mi))
override _mi := $(subst r,,$(_mi))
override _mi := $(subst s,,$(_mi))
override _mi := $(subst t,,$(_mi))
override _mi := $(subst u,,$(_mi))
override _mi := $(subst v,,$(_mi))
override _mi := $(subst w,,$(_mi))
override _mi := $(subst x,,$(_mi))
override _mi := $(subst y,,$(_mi))
override _mi := $(subst z,,$(_mi))
override _mi := $(subst .,,$(_mi))
override _mi := $(subst _,,$(_mi))
override _mi := $(subst /,,$(_mi))
override _mi := $(subst +,,$(_mi))
override _mi := $(subst -,,$(_mi))
override _mi := $(subst $(space),x,$(_mi))
ifneq ($(strip $(_mi)),)
$(error refusing Makefile path)
endif
override MI_MAKEFILE_CHECK := $(realpath $(MI_ROOT)/Makefile)
ifneq ($(MI_MAKEFILE),$(MI_MAKEFILE_CHECK))
$(error refusing Makefile path)
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
	@$(enter_root); go vet -printf.funcs=Notef ./...
	@$(enter_root); go test -race -count=1 ./...
	@$(enter_root); \
	stamp=bin/.mi-artifact-stamp; \
	if [ ! -f "$$stamp" ]; then echo "make test: missing artifact stamp" >&2; exit 1; fi; \
	{ IFS= read -r bin_state; IFS= read -r bin_sum; IFS= read -r bin_id; IFS= read -r bin_x; IFS= read -r cov_state; IFS= read -r cov_sum; IFS= read -r cov_id; } < "$$stamp"; \
	rm -f "$$stamp"; \
	real_bin=bin/model-inspector; real_cov=coverage.out; \
	classify() { if [ -L "$$1" ]; then printf symlink; elif [ -d "$$1" ]; then printf dir; elif [ -f "$$1" ]; then printf file; elif [ -e "$$1" ]; then printf other; else printf absent; fi; }; \
	now=$$(classify "$$real_bin"); \
	if [ "$$now" != "$$bin_state" ]; then echo "make test changed bin/model-inspector (was $$bin_state, now $$now)" >&2; exit 1; fi; \
	if [ "$$bin_state" != absent ]; then id=$$(ls -id "$$real_bin" | awk '{print $$1}'); if [ "$$id" != "$$bin_id" ]; then echo "make test replaced bin/model-inspector" >&2; exit 1; fi; fi; \
	if [ "$$bin_state" = file ]; then sum=$$(cksum < "$$real_bin"); if [ "$$sum" != "$$bin_sum" ]; then echo "make test modified bin/model-inspector" >&2; exit 1; fi; if [ "$$bin_x" = 1 ] && [ ! -x "$$real_bin" ]; then echo "make test cleared the executable bit on bin/model-inspector" >&2; exit 1; fi; fi; \
	now=$$(classify "$$real_cov"); \
	if [ "$$now" != "$$cov_state" ]; then echo "make test changed coverage.out (was $$cov_state, now $$now)" >&2; exit 1; fi; \
	if [ "$$cov_state" != absent ]; then id=$$(ls -id "$$real_cov" | awk '{print $$1}'); if [ "$$id" != "$$cov_id" ]; then echo "make test replaced coverage.out" >&2; exit 1; fi; fi; \
	if [ "$$cov_state" = file ]; then sum=$$(cksum < "$$real_cov"); if [ "$$sum" != "$$cov_sum" ]; then echo "make test modified coverage.out" >&2; exit 1; fi; fi

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
	restore() { \
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
	real_bin=$$MI_ROOT/bin/model-inspector; \
	real_cov=$$MI_ROOT/coverage.out; \
	classify() { \
	  if [ -L "$$1" ]; then printf symlink; \
	  elif [ -d "$$1" ]; then printf dir; \
	  elif [ -f "$$1" ]; then printf file; \
	  elif [ -e "$$1" ]; then printf other; \
	  else printf absent; \
	  fi; \
	}; \
	bin_state=$$(classify "$$real_bin"); \
	cov_state=$$(classify "$$real_cov"); \
	bin_sum=; bin_id=; bin_x=; \
	cov_sum=; cov_id=; \
	if [ "$$bin_state" != absent ]; then \
	  bin_id=$$(ls -id "$$real_bin" | awk '{print $$1}'); \
	fi; \
	if [ "$$bin_state" = file ]; then \
	  bin_sum=$$(cksum < "$$real_bin"); \
	  if [ -x "$$real_bin" ]; then bin_x=1; else bin_x=0; fi; \
	fi; \
	if [ "$$cov_state" != absent ]; then \
	  cov_id=$$(ls -id "$$real_cov" | awk '{print $$1}'); \
	fi; \
	if [ "$$cov_state" = file ]; then \
	  cov_sum=$$(cksum < "$$real_cov"); \
	fi; \
	assert_real_untouched() { \
	  label=$$1; \
	  now=$$(classify "$$real_bin"); \
	  if [ "$$now" != "$$bin_state" ]; then \
	    echo "clean changed bin/model-inspector ($$label): was $$bin_state now $$now" >&2; exit 1; \
	  fi; \
	  if [ "$$bin_state" != absent ]; then \
	    id=$$(ls -id "$$real_bin" | awk '{print $$1}'); \
	    if [ "$$id" != "$$bin_id" ]; then \
	      echo "clean replaced bin/model-inspector ($$label)" >&2; exit 1; \
	    fi; \
	  fi; \
	  if [ "$$bin_state" = file ]; then \
	    sum=$$(cksum < "$$real_bin"); \
	    if [ "$$sum" != "$$bin_sum" ]; then \
	      echo "clean modified bin/model-inspector ($$label)" >&2; exit 1; \
	    fi; \
	    if [ "$$bin_x" = 1 ] && [ ! -x "$$real_bin" ]; then \
	      echo "clean cleared the executable bit on bin/model-inspector ($$label)" >&2; exit 1; \
	    fi; \
	  fi; \
	  now=$$(classify "$$real_cov"); \
	  if [ "$$now" != "$$cov_state" ]; then \
	    echo "clean changed coverage.out ($$label): was $$cov_state now $$now" >&2; exit 1; \
	  fi; \
	  if [ "$$cov_state" != absent ]; then \
	    id=$$(ls -id "$$real_cov" | awk '{print $$1}'); \
	    if [ "$$id" != "$$cov_id" ]; then \
	      echo "clean replaced coverage.out ($$label)" >&2; exit 1; \
	    fi; \
	  fi; \
	  if [ "$$cov_state" = file ]; then \
	    sum=$$(cksum < "$$real_cov"); \
	    if [ "$$sum" != "$$cov_sum" ]; then \
	      echo "clean modified coverage.out ($$label)" >&2; exit 1; \
	    fi; \
	  fi; \
	}; \
	scratch=$$(mktemp -d "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	mkdir -p "$$scratch/caller/bin" "$$scratch/caller/sub/bin" "$$scratch/tree/internal/bin" "$$scratch/cov/bin" "$$scratch/cov/coverage.out"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/tree/Makefile"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/cov/Makefile"; \
	printf keep > "$$scratch/caller/bin/model-inspector"; \
	printf keep > "$$scratch/caller/coverage.out"; \
	printf keep > "$$scratch/caller/sub/bin/model-inspector"; \
	printf keep > "$$scratch/caller/sub/coverage.out"; \
	printf keep > "$$scratch/tree/internal/bin/model-inspector"; \
	printf keep > "$$scratch/tree/internal/coverage.out"; \
	printf x > "$$scratch/cov/bin/keep"; \
	seed_tree() { \
	  rm -rf "$$scratch/tree/bin"; \
	  rm -f "$$scratch/tree/coverage.out"; \
	  mkdir -p "$$scratch/tree/bin"; \
	  printf repo > "$$scratch/tree/bin/model-inspector"; \
	  printf repo > "$$scratch/tree/coverage.out"; \
	}; \
	assert_tree_cleaned() { \
	  if [ -e "$$scratch/tree/bin" ] || [ -L "$$scratch/tree/bin" ] || [ -e "$$scratch/tree/coverage.out" ] || [ -L "$$scratch/tree/coverage.out" ]; then \
	    echo "clean left the scratch makefile tree's bin/ or coverage.out ($$1)" >&2; exit 1; \
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
	    $(MAKE) -C "$$dir" -f "$$scratch/tree/Makefile" -s --no-print-directory clean "$$@" >"$$tmp.out" 2>&1; \
	}; \
	seed_tree; \
	if ! run_clean_from "$$scratch/caller" BIN=.git; then \
	  echo "clean from a scratch directory failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "scratch directory" "$$scratch/caller"; \
	assert_tree_cleaned "scratch directory"; \
	assert_real_untouched "scratch directory"; \
	seed_tree; \
	if ! run_clean_from "$$scratch/caller/sub" BIN=bin/.; then \
	  echo "clean from a subdirectory failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "scratch subdirectory" "$$scratch/caller/sub"; \
	assert_survives "scratch directory" "$$scratch/caller"; \
	assert_tree_cleaned "scratch subdirectory"; \
	assert_real_untouched "scratch subdirectory"; \
	seed_tree; \
	if ! ( cd "$$scratch/tree/internal" && env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -f ../Makefile -s --no-print-directory clean ) >"$$tmp.out" 2>&1; then \
	  echo "clean from a scratch subdirectory failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "scratch makefile subdirectory" "$$scratch/tree/internal"; \
	assert_tree_cleaned "scratch makefile subdirectory"; \
	assert_real_untouched "scratch makefile subdirectory"; \
	seed_tree; \
	if ! run_clean_from "$$scratch/caller" MI_ROOT="$$scratch/caller"; then \
	  echo "clean with MI_ROOT override failed" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "MI_ROOT override" "$$scratch/caller"; \
	assert_tree_cleaned "MI_ROOT override"; \
	assert_real_untouched "MI_ROOT override"; \
	if env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$scratch/caller" -f "$$scratch/cov/Makefile" -s --no-print-directory clean >"$$tmp.out" 2>&1; then \
	  echo "directory coverage.out was not refused" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if [ -e "$$scratch/cov/bin" ] || [ -L "$$scratch/cov/bin" ]; then \
	  echo "directory coverage.out stopped bin/ removal" >&2; exit 1; \
	fi; \
	if [ ! -d "$$scratch/cov/coverage.out" ] || [ -L "$$scratch/cov/coverage.out" ]; then \
	  echo "directory coverage.out was removed" >&2; exit 1; \
	fi; \
	if ! grep -q 'coverage.out is a directory' "$$tmp.out"; then \
	  echo "directory coverage.out produced no warning" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	assert_survives "caller during directory coverage.out" "$$scratch/caller"; \
	assert_real_untouched "directory coverage.out"; \
	mkdir -p "$$scratch/p6x/a b/bin" "$$scratch/p6x b/bin" "$$scratch/cwd"; \
	printf KEEP_DECOY > "$$scratch/p6x b/bin/model-inspector"; \
	printf KEEP_DECOY > "$$scratch/p6x b/coverage.out"; \
	printf KEEP_REPO > "$$scratch/p6x/a b/bin/model-inspector"; \
	printf KEEP_REPO > "$$scratch/p6x/a b/coverage.out"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/p6x/a b/Makefile"; \
	dollar='d$$(touch$${IFS}PWNED_BY_PATH)'; \
	mkdir -p "$$scratch/$$dollar"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/$$dollar/Makefile"; \
	refuse_path() { \
	  label=$$1; dir=$$2; goal=$$3; from=$$4; \
	  if env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	      -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	      -u MI_LDFLAGS -u LDFLAGS \
	      MAKEFLAGS= MAKEOVERRIDES= \
	      $(MAKE) -C "$$from" -f "$$dir/Makefile" -s --no-print-directory "$$goal" >"$$tmp.out" 2>&1; then \
	    echo "$$label $$goal was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	  if ! grep -q 'refusing Makefile path' "$$tmp.out"; then \
	    echo "$$label $$goal was not a clean refusal" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	}; \
	refuse_path space "$$scratch/p6x/a b" clean "$$scratch/p6x/a b"; \
	refuse_path space "$$scratch/p6x/a b" test "$$scratch/cwd"; \
	if [ "$$(cat "$$scratch/p6x b/bin/model-inspector")" != KEEP_DECOY ] \
	    || [ "$$(cat "$$scratch/p6x b/coverage.out")" != KEEP_DECOY ] \
	    || [ "$$(cat "$$scratch/p6x/a b/bin/model-inspector")" != KEEP_REPO ] \
	    || [ "$$(cat "$$scratch/p6x/a b/coverage.out")" != KEEP_REPO ]; then \
	  echo "space path changed bin/ or coverage.out" >&2; exit 1; \
	fi; \
	refuse_path 'dollar' "$$scratch/$$dollar" test "$$scratch/$$dollar"; \
	refuse_path 'dollar' "$$scratch/$$dollar" test "$$scratch/cwd"; \
	for pwn in "$$scratch/PWNED_BY_PATH" "$$scratch/cwd/PWNED_BY_PATH" \
	    "$$scratch/$$dollar/PWNED_BY_PATH" "$$scratch/p6x/a b/PWNED_BY_PATH" \
	    /tmp/PWNED_BY_PATH "$$MI_ROOT/PWNED_BY_PATH"; do \
	  if [ -e "$$pwn" ]; then echo "hostile path created $$pwn" >&2; exit 1; fi; \
	done; \
	mkdir -p "$$scratch/p6z/nope b" "$$scratch/p6zcwd/b/bin"; \
	printf KEEP > "$$scratch/p6zcwd/b/bin/model-inspector"; \
	printf KEEP > "$$scratch/p6zcwd/b/coverage.out"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/p6z/nope b/Makefile"; \
	cp "$$MI_ROOT/Makefile" "$$scratch/p6zcwd/b/Makefile"; \
	if env -u BIN -u VERSION -u COMMIT -u FUZZTIME \
	    -u VERSION_RAW -u COMMIT_RAW -u FUZZTIME_RAW -u BIN_RAW \
	    -u MI_LDFLAGS -u LDFLAGS \
	    MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -C "$$scratch/p6zcwd" -f "$$scratch/p6z/nope b/Makefile" -s --no-print-directory clean >"$$tmp.out" 2>&1; then \
	  echo "nope b path was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if ! grep -q 'refusing Makefile path' "$$tmp.out"; then \
	  echo "nope b path was not a clean refusal" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi; \
	if [ "$$(cat "$$scratch/p6zcwd/b/bin/model-inspector")" != KEEP ] \
	    || [ "$$(cat "$$scratch/p6zcwd/b/coverage.out")" != KEEP ]; then \
	  echo "nope b path removed the caller b/ artifacts" >&2; exit 1; \
	fi; \
	assert_real_untouched "hostile path"; \
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
	assert_real_untouched "symlinked bin"; \
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
	  assert_real_untouched "$$label"; \
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
	fi; \
	assert_real_untouched "end of clean self-test"; \
	mkdir -p bin; \
	printf '%s\n' "$$bin_state" "$$bin_sum" "$$bin_id" "$$bin_x" "$$cov_state" "$$cov_sum" "$$cov_id" > bin/.mi-artifact-stamp
