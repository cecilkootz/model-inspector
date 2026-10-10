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

.PHONY: test build clean cover fuzz check-overrides check-env
.DEFAULT_GOAL := test

# Vet and test every package. Synthetic fixtures are generated in-process;
# a clean checkout does not need any external checkpoint. check-overrides
# rejects hostile VERSION/COMMIT values before the linker runs.
test: check-overrides
	go vet ./...
	go test -race -count=1 ./...

build:
	@set -eu; \
	mkdir -p -- "$$(dirname -- "$$BIN_RAW")"; \
	if [ -n "$$MI_LDFLAGS" ]; then \
		go build -ldflags "$$MI_LDFLAGS" -o "$$BIN_RAW" ./cmd/model-inspector; \
	else \
		go build -o "$$BIN_RAW" ./cmd/model-inspector; \
	fi

# Remove coverage.out, the built binary, and a real bin/ directory.
# coverage.out is removed first, so a later rm failure still drops it.
# A directory BIN is removed with rm -rf. BIN must be a relative path inside
# this repository. Absolute paths, a path segment that is exactly "..", and
# a path whose parent resolves outside the repo are refused. "bin/v1..2" is
# a single segment and is allowed. A symlinked bin/ is removed as a symlink.
clean:
	@set -eu; \
	repo=$$(pwd -P); \
	rm -f coverage.out; \
	case "$$BIN_RAW" in \
	  ""|/*) echo "make clean: BIN must be a relative path inside the repo" >&2; exit 1 ;; \
	esac; \
	set -f; \
	oldifs=$$IFS; \
	IFS=/; \
	set -- $$BIN_RAW; \
	IFS=$$oldifs; \
	set +f; \
	for seg in "$$@"; do \
	  if [ "$$seg" = ".." ]; then \
	    echo "make clean: BIN must not contain a .. path segment" >&2; \
	    exit 1; \
	  fi; \
	done; \
	if [ -e "$$BIN_RAW" ] || [ -L "$$BIN_RAW" ]; then \
	  parent=$$(dirname -- "$$BIN_RAW"); \
	  base=$$(basename -- "$$BIN_RAW"); \
	  resolved=$$(cd -- "$$parent" && pwd -P) || { echo "make clean: cannot resolve $$BIN_RAW" >&2; exit 1; }; \
	  case "$$resolved/" in \
	    "$$repo/"*) ;; \
	    *) echo "make clean: refusing to remove $$BIN_RAW (outside the repo)" >&2; exit 1 ;; \
	  esac; \
	  target=$$resolved/$$base; \
	  if [ -d "$$target" ] && [ ! -L "$$target" ]; then \
	    rtarget=$$(cd -- "$$target" && pwd -P); \
	    if [ "$$rtarget" = "$$repo" ]; then \
	      echo "make clean: refusing to remove the repository root" >&2; \
	      exit 1; \
	    fi; \
	    rm -rf -- "$$target"; \
	  else \
	    rm -f -- "$$target"; \
	  fi; \
	fi; \
	if [ -L bin ]; then \
	  rm -f bin; \
	elif [ -d bin ]; then \
	  rm -rf bin; \
	fi

# Expands recipe exports without building or deleting. check-overrides uses it.
check-env:
	@:

cover:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Go runs one fuzz target per `go test` invocation, so list every Fuzz*
# function and call each package on its own. Regular tests in that package
# are not repeated (-run matches nothing).
fuzz:
	@set -eu; \
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
	tmp=$$(mktemp "$${TMPDIR:-/tmp}/model-inspector.XXXXXX"); \
	ran="$$tmp.ran"; \
	spaced=; \
	restore() { \
	  if [ -n "$$spaced" ]; then rm -rf "$$spaced"; fi; \
	  if [ -d "$$tmp.bin-save" ] || [ -L "$$tmp.bin-save" ]; then \
	    rm -rf bin; mv "$$tmp.bin-save" bin; \
	  fi; \
	  if [ -e "$$tmp.cov-save" ]; then mv "$$tmp.cov-save" coverage.out; fi; \
	  rm -rf .qa-clean-dir; \
	  rm -f "$$tmp" "$$tmp.out" "$$tmp.script" "$$ran"; \
	}; \
	trap restore EXIT; \
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
	if [ -d bin ] || [ -L bin ]; then mv bin "$$tmp.bin-save"; fi; \
	if [ -e coverage.out ]; then mv coverage.out "$$tmp.cov-save"; fi; \
	mkdir -p .qa-clean-dir; \
	printf x > coverage.out; \
	clean_make clean BIN='.qa-clean-dir'; \
	if [ -e coverage.out ] || [ -e .qa-clean-dir ]; then \
	  echo "directory BIN left coverage.out or the directory" >&2; exit 1; \
	fi; \
	clean_make clean BIN='bin/v1..2'; \
	if clean_make clean BIN='foo/../bar' >"$$tmp.out" 2>&1; then \
	  echo ".. path segment was accepted" >&2; exit 1; \
	fi
