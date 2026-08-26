.PHONY: build test lint install uninstall fixtures clean

# Installation prefix. Override for a user-local install that needs no
# elevation, e.g. `make install PREFIX=$HOME/.local`.
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Elevate only when BINDIR is not writable, so a user-local PREFIX never
# invokes sudo. BINDIR may not exist yet, so test the nearest existing
# ancestor — testing BINDIR directly reports "not writable" for any missing
# directory and would prompt for a password we do not need.
SUDO := $(shell d="$(BINDIR)"; \
	while [ ! -d "$$d" ] && [ "$$d" != "/" ] && [ "$$d" != "." ]; do \
		d=$$(dirname "$$d"); \
	done; \
	[ -w "$$d" ] || echo sudo)

# build produces ./exshell from the module root.
build:
	go build -o ./exshell .

# test runs the full suite. Race detection for the viewer specifically is a
# separate, slower invocation (see README) since Bubble Tea's own goroutines
# make it the package most worth checking under -race.
test:
	go test ./...

# lint is honest about what it runs: go vet plus a gofmt diff check. No
# external linter (golangci-lint et al.) is assumed to be installed.
lint:
	go vet ./...
	@fmt_files="$$(gofmt -l .)"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt needs to be run on:"; \
		echo "$$fmt_files"; \
		exit 1; \
	fi

# install builds for the host platform and installs the binary into BINDIR
# ($(PREFIX)/bin, default /usr/local/bin).
#
# The build runs as the invoking user and only the copy is elevated. Running
# the whole target under `sudo make install` would compile as root and leave
# root-owned files in the module cache (~/go/pkg/mod), which breaks later
# builds as yourself — so run this as plain `make install` and let it prompt
# for the copy alone.
#
# Deliberately a native build: cross-compiling and then installing the result
# into this machine's BINDIR would produce a binary that cannot run here.
install: build
	$(SUDO) install -d "$(BINDIR)"
	$(SUDO) install -m 0755 ./exshell "$(BINDIR)/exshell"
	@echo "installed $(BINDIR)/exshell"

# uninstall removes what install placed, using the same PREFIX/BINDIR.
uninstall:
	$(SUDO) rm -f "$(BINDIR)/exshell"
	@echo "removed $(BINDIR)/exshell"

# fixtures (re)generates testdata/*.csv and testdata/*.xlsx via
# cmd/genfixtures. big.csv is git-ignored; the rest are committed so the
# repo is usable without running this target.
fixtures:
	go run ./cmd/genfixtures --dir testdata

clean:
	rm -f ./exshell
