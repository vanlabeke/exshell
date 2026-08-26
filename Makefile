.PHONY: build test lint install fixtures clean

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

# install builds genfixtures and installs exshell via `go install`, so it
# lands on $GOPATH/bin (or $GOBIN) like any other Go tool.
install:
	go install .

# fixtures (re)generates testdata/*.csv and testdata/*.xlsx via
# cmd/genfixtures. big.csv is git-ignored; the rest are committed so the
# repo is usable without running this target.
fixtures:
	go run ./cmd/genfixtures --dir testdata

clean:
	rm -f ./exshell
