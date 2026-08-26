// Command exshell is a terminal viewer for CSV and XLSX files.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"vanlabeke.dev/exshell/internal/cli"
)

// Build metadata, overwritten at link time by the release pipeline:
//
//	-X main.version=1.2.3 -X main.commit=abc1234 -X main.date=2026-08-26
//
// These must stay `var` — the linker's -X flag cannot write to a const. The
// names are GoReleaser's defaults, so its stock ldflags template populates
// them with no extra configuration.
var (
	version = ""
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, buildVersion()))
}

// buildVersion renders what --version prints.
//
// A release build reports its tag and provenance: "1.2.3 (abc1234, 2026-08-26)".
// A build from source has no ldflags, so it falls back to the module version
// the Go toolchain records — "v0.0.0-20260826..." for `go install`, or the
// bare "(devel)" for `go build` in a working tree. Reporting a hardcoded
// number for an untagged local build would be a lie, and a version string
// nobody can map back to a commit is worse than no version string.
func buildVersion() string {
	if version == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			return info.Main.Version
		}
		return "devel"
	}
	if commit == "" {
		return version
	}
	if date == "" {
		return fmt.Sprintf("%s (%s)", version, commit)
	}
	return fmt.Sprintf("%s (%s, %s)", version, commit, date)
}
