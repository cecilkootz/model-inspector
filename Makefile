# GNU make is required. On macOS, use gmake when /usr/bin/make is BSD make.
#
# Bare `make` runs `test` (override check, vet, and race tests). That is the
# historical default, plus the override check below.
#
# VERSION, COMMIT, FUZZTIME, and BIN may be set in the environment or on the
# make command line. A command-line assignment wins over the environment.
# VERSION and COMMIT are optional -ldflags -X overrides. When they are unset,
# the binary keeps internal/version.Version and the VCS stamp from the Go
# toolchain (runtime/debug.ReadBuildInfo). Overrides must match
# ^[0-9A-Za-z.+_-]+$ so they cannot change the shell command or the linker
# command line. FUZZTIME uses that same character set.

MODULE := origin.cursor.com/cecilkootz/model-inspector

# Command-line variables are exported into recipe environments, and that
# export expands $(shell) and other functions. Drop them so a hostile value
# cannot run during a later recipe. Recipes read the simple copies below.
unexport VERSION
unexport COMMIT
unexport FUZZTIME
unexport BIN

VERSION_RAW := $(value VERSION)
COMMIT_RAW := $(value COMMIT)
FUZZTIME_RAW := $(value FUZZTIME)
BIN_RAW := $(value BIN)

space := $(subst ,, )
ok_chars := 0 1 2 3 4 5 6 7 8 9 A B C D E F G H I J K L M N O P Q R S T U V W X Y Z a b c d e f g h i j k l m n o p q r s t u v w x y z . + _ -
remain = $(if $(2),$(call remain,$(subst $(firstword $(2)),,$(1)),$(wordlist 2,200,$(2))),$(1))
# Non-empty when $(2) contains a character outside ^[0-9A-Za-z.+_-]+$
# (spaces count; $(if) alone treats a whitespace-only string as empty).
bad_token = $(subst $(space),x,$(call remain,$(1),$(ok_chars)))
assert_token = $(if $(call bad_token,$(2)),$(error $(1) must match ^[0-9A-Za-z.+_-]+$$))

ifeq ($(strip $(FUZZTIME_RAW)),)
FUZZTIME_RAW := 10s
endif
ifeq ($(strip $(BIN_RAW)),)
BIN_RAW := bin/model-inspector
endif

_ := $(call assert_token,FUZZTIME,$(FUZZTIME_RAW))
define newline


endef
_ := $(if $(findstring $(newline),$(BIN_RAW)),$(error BIN must be a single line))

# Append -X only after the token check accepts the value. $(error) aborts
# before $(eval), so a rejected value is never written into LDFLAGS.
LDFLAGS :=
_ := $(if $(strip $(VERSION_RAW)),$(call assert_token,VERSION,$(VERSION_RAW))$(eval LDFLAGS += -X $(MODULE)/internal/version.Version=$(VERSION_RAW)))
_ := $(if $(strip $(COMMIT_RAW)),$(call assert_token,COMMIT,$(COMMIT_RAW))$(eval LDFLAGS += -X $(MODULE)/internal/version.Commit=$(COMMIT_RAW)))

export BIN_RAW
export LDFLAGS

.PHONY: test build clean cover fuzz check-overrides
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
	if [ -n "$$LDFLAGS" ]; then \
		go build -ldflags "$$LDFLAGS" -o "$$BIN_RAW" ./cmd/model-inspector; \
	else \
		go build -o "$$BIN_RAW" ./cmd/model-inspector; \
	fi

# Remove coverage.out, the built binary, and a real bin/ directory.
# BIN must be a relative path inside this repository: absolute paths and
# ".." are refused, and a path whose parent resolves outside the repo is
# refused. A symlinked bin/ is removed as a symlink.
clean:
	@set -eu; \
	repo=$$(pwd -P); \
	case "$$BIN_RAW" in \
	  ""|/*) echo "make clean: BIN must be a relative path inside the repo" >&2; exit 1 ;; \
	esac; \
	case "$$BIN_RAW" in \
	  *..*) echo "make clean: BIN must not contain .." >&2; exit 1 ;; \
	esac; \
	if [ -e "$$BIN_RAW" ] || [ -L "$$BIN_RAW" ]; then \
	  parent=$$(dirname -- "$$BIN_RAW"); \
	  base=$$(basename -- "$$BIN_RAW"); \
	  resolved=$$(cd -- "$$parent" && pwd -P) || { echo "make clean: cannot resolve $$BIN_RAW" >&2; exit 1; }; \
	  case "$$resolved/" in \
	    "$$repo/"*) rm -f -- "$$resolved/$$base" ;; \
	    *) echo "make clean: refusing to remove $$BIN_RAW (outside the repo)" >&2; exit 1 ;; \
	  esac; \
	fi; \
	rm -f coverage.out; \
	if [ -L bin ]; then \
	  rm -f bin; \
	elif [ -d bin ]; then \
	  rm -rf bin; \
	fi

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
	trap 'rm -f "$$tmp" "$$tmp.out" "$$tmp.script" "$$ran"' EXIT; \
	clean_make() { \
	  env -u VERSION -u COMMIT -u FUZZTIME -u BIN MAKEFLAGS= MAKEOVERRIDES= \
	    $(MAKE) -s --no-print-directory "$$@"; \
	}; \
	reject() { \
	  val=$$1; label=$$2; shift 2; \
	  if clean_make "$$@" VERSION="$$val" >"$$tmp.out" 2>&1; then \
	    echo "$$label was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	  fi; \
	  if [ -e "$$ran" ]; then \
	    echo "$$label executed a command" >&2; exit 1; \
	  fi; \
	}; \
	val=$$(printf '%s' '0.1.0$$(shell touch '"$$ran"')'); \
	reject "$$val" shell build; \
	val=$$(printf '%s' '0.1.0`touch '"$$ran"'`'); \
	reject "$$val" backticks build; \
	val=$$(printf '%s' '$${HOME}'); \
	reject "$$val" home build; \
	val=$$(printf '%s' '0.1.0 -X main.injected=yes'); \
	reject "$$val" extra-x build; \
	printf '#!/bin/sh\ntouch %s\n' "$$ran" > "$$tmp.script"; \
	chmod +x "$$tmp.script"; \
	val=$$(printf '%s' "-linkmode=external -extld=$$tmp.script"); \
	reject "$$val" extld build; \
	ldflags_line() { \
	  clean_make -np "$$@" | awk '/^LDFLAGS :=/ { print; exit }'; \
	}; \
	out=$$(ldflags_line build); \
	case "$$out" in \
	  "LDFLAGS :="|"LDFLAGS := ") ;; \
	  *) echo "default build sets LDFLAGS: $$out" >&2; exit 1 ;; \
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
	trap 'rm -f "$$tmp" "$$tmp.out" "$$tmp.script" "$$ran"; rm -rf "$$spaced"' EXIT; \
	clean_make build BIN="$$spaced/my tool"; \
	test -x "$$spaced/my tool"; \
	if clean_make -n fuzz FUZZTIME='10s;touch' >"$$tmp.out" 2>&1; then \
	  echo "hostile FUZZTIME was accepted" >&2; cat "$$tmp.out" >&2; exit 1; \
	fi
