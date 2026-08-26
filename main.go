// Command exshell is a terminal viewer for CSV and XLSX files.
package main

import (
	"os"

	"vanlabeke.dev/exshell/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
