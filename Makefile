# GNU make is required. On macOS, use gmake when /usr/bin/make is BSD make.
#
# Bare `make` runs `test` (vet + race tests). That is the historical default.

MODULE := origin.cursor.com/cecilkootz/model-inspector
BIN := bin/model-inspector

# Default matches internal/version.Version. Command-line VERSION= overrides it.
VERSION := $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' internal/version/version.go)
ifeq ($(strip $(VERSION)),)
$(error could not read Version from internal/version/version.go)
endif

# Short git commit inside a checkout. Outside one (no .git, git missing, or
# rev-parse fails) this is "unknown", matching version.Commit's default.
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# Each fuzz target runs for this long. Example: make fuzz FUZZTIME=30s
FUZZTIME ?= 10s

LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT)

.PHONY: test build clean cover fuzz
.DEFAULT_GOAL := test

# Vet and test every package. Synthetic fixtures are generated in-process;
# a clean checkout does not need any external checkpoint.
test:
	go vet ./...
	go test -race -count=1 ./...

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/model-inspector

clean:
	rm -rf bin

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
		printf 'fuzz %s %s (%s)\n' "$$pkg" "$$name" "$(FUZZTIME)"; \
		go test "$$pkg" -count=1 -run '^$$' -fuzz "^$$name$$" -fuzztime "$(FUZZTIME)" || exit 1; \
	done < "$$tmp"
